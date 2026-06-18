//  PBX策略：路由策略+负载策略+会话管理策略
package policy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

var (
	ErrTenantNotFound          = errors.New("tenant not found")
	ErrTenantDisabled          = errors.New("tenant disabled")
	ErrTenantDenied            = errors.New("tenant denied")
	ErrRateLimitExceeded       = errors.New("rate limit exceeded")
	ErrConcurrentLimitExceeded = errors.New("concurrent call limit exceeded")
)

type TenantContext struct {
	TenantID     string
	Status       model.TenantStatus
	Tier         model.TenantTier
	MaxCalls     int
	MaxAIWorkers int
	Features     []string
	Settings     map[string]string
}

type RateLimit struct {
	Limit  int
	Window time.Duration
}

type AccessList struct {
	Whitelist []string
	Blacklist []string
}

type PolicyEngine interface {
	CheckRateLimit(ctx context.Context, tenantID, resource string) (bool, error)
	CheckAccessList(ctx context.Context, tenantID, target string) (bool, error)
	Authorize(ctx context.Context, tenantID, action, resource string) (bool, error)
	ValidateTenant(ctx context.Context, tenantID string) (*TenantContext, error)
}

type MemoryEngine struct {
	mu          sync.Mutex
	tenants     map[string]TenantContext
	rateLimits  map[string]RateLimit
	rateWindows map[string][]time.Time
	accessLists map[string]AccessList
	permissions map[string][]model.Permission
	callSlots   map[string]int
}

// NewMemoryEngine 创建内存策略引擎。
func NewMemoryEngine() *MemoryEngine {
	return &MemoryEngine{
		tenants:     map[string]TenantContext{},
		rateLimits:  map[string]RateLimit{},
		rateWindows: map[string][]time.Time{},
		accessLists: map[string]AccessList{},
		permissions: map[string][]model.Permission{},
		callSlots:   map[string]int{},
	}
}

// SetTenant 设置租户上下文（含 maxCalls、tier 等信息）。
func (e *MemoryEngine) SetTenant(ctx TenantContext) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tenants[ctx.TenantID] = ctx
}

// SetRateLimit 设置某租户某资源的速率限制。
func (e *MemoryEngine) SetRateLimit(tenantID, resource string, limit RateLimit) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rateLimits[tenantResourceKey(tenantID, resource)] = limit
}

// SetAccessList 设置接入黑/白名单（会归一化号码）。
func (e *MemoryEngine) SetAccessList(tenantID string, list AccessList) {
	e.mu.Lock()
	defer e.mu.Unlock()
	list.Whitelist = normalizeNumbers(list.Whitelist)
	list.Blacklist = normalizeNumbers(list.Blacklist)
	e.accessLists[tenantID] = list
}

// SetPermissions 设置租户 RBAC 权限表。
func (e *MemoryEngine) SetPermissions(tenantID string, permissions []model.Permission) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.permissions[tenantID] = append([]model.Permission(nil), permissions...)
}

// ValidateTenant 校验租户存在且未被禁用。
func (e *MemoryEngine) ValidateTenant(ctx context.Context, tenantID string) (*TenantContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	tenant, ok := e.tenants[tenantID]
	if !ok {
		return nil, ErrTenantNotFound
	}
	if tenant.Status == model.TenantStatusDisabled {
		return nil, ErrTenantDisabled
	}
	return &tenant, nil
}

// CheckRateLimit 滑动窗口限流，返回是否允许此次请求。
func (e *MemoryEngine) CheckRateLimit(ctx context.Context, tenantID, resource string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	key := tenantResourceKey(tenantID, resource)
	limit, ok := e.rateLimits[key]
	if !ok || limit.Limit <= 0 {
		return true, nil
	}
	if limit.Window <= 0 {
		limit.Window = time.Second
	}

	now := time.Now()
	windowStart := now.Add(-limit.Window)
	events := e.rateWindows[key]
	kept := events[:0]
	for _, at := range events {
		if at.After(windowStart) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limit.Limit {
		e.rateWindows[key] = kept
		return false, nil
	}
	e.rateWindows[key] = append(kept, now)
	return true, nil
}

// CheckAccessList 检查号码接入权限：黑名单优先，白名单有值时仅允许白名单。
func (e *MemoryEngine) CheckAccessList(ctx context.Context, tenantID, target string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	list := e.accessLists[tenantID]
	normalized := NormalizeNumber(target)
	if contains(list.Blacklist, normalized) {
		return false, nil
	}
	if len(list.Whitelist) > 0 {
		return contains(list.Whitelist, normalized), nil
	}
	return true, nil
}

// Authorize RBAC 鉴权：action 和 resource 支持 * 通配符。
func (e *MemoryEngine) Authorize(ctx context.Context, tenantID, action, resource string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, permission := range e.permissions[tenantID] {
		if (permission.Action == action || permission.Action == "*") &&
			(permission.Resource == resource || permission.Resource == "*") {
			return true, nil
		}
	}
	return false, nil
}

// EnsureTenantResource 校验操作者与资源归属的租户一致（租户隔离）。
func (e *MemoryEngine) EnsureTenantResource(actorTenantID, resourceTenantID string) error {
	if actorTenantID != resourceTenantID {
		return ErrTenantDenied
	}
	return nil
}

// AcquireCallSlot 尝试占一个并发通话槽位，达到 maxCalls 限制时返回错误。
func (e *MemoryEngine) AcquireCallSlot(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	tenant, ok := e.tenants[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	if tenant.MaxCalls > 0 && e.callSlots[tenantID] >= tenant.MaxCalls {
		return ErrConcurrentLimitExceeded
	}
	e.callSlots[tenantID]++
	return nil
}

// ReleaseCallSlot 释放一个并发通话槽位。
func (e *MemoryEngine) ReleaseCallSlot(tenantID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.callSlots[tenantID] > 0 {
		e.callSlots[tenantID]--
	}
}

// NormalizeNumber 归一号码：保留前导 + 号，移除所有非数字字符。
func NormalizeNumber(value string) string {
	value = strings.TrimSpace(value)
	prefix := ""
	if strings.HasPrefix(value, "+") {
		prefix = "+"
	}
	var out strings.Builder
	out.WriteString(prefix)
	for _, r := range value {
		if unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// tenantResourceKey 生成限流键：tenantID:resource。
func tenantResourceKey(tenantID, resource string) string {
	return tenantID + ":" + resource
}

// normalizeNumbers 批量归一号码列表。
func normalizeNumbers(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, NormalizeNumber(value))
	}
	return out
}

// contains 线性查找目标值是否存在。
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

