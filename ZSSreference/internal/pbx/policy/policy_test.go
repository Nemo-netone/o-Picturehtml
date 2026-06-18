//  PBX策略：路由策略+负载策略+会话管理策略
package policy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/policy"
)

// 作用: 验证 Test Policy_ Validate Tenant_ Disabled 场景的行为。
func TestPolicy_ValidateTenant_Disabled(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetTenant(policy.TenantContext{TenantID: "tenant-a", Status: model.TenantStatusDisabled})

	_, err := engine.ValidateTenant(context.Background(), "tenant-a")
	if !errors.Is(err, policy.ErrTenantDisabled) {
		t.Fatalf("expected disabled tenant, got %v", err)
	}
}

// 作用: 验证 Test Policy_ Rate Limit_ Exceeded 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestPolicy_RateLimit_Exceeded(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetRateLimit("tenant-a", "route", policy.RateLimit{Limit: 1, Window: time.Second})

	if ok, err := engine.CheckRateLimit(context.Background(), "tenant-a", "route"); err != nil || !ok {
		t.Fatalf("first request should pass: ok=%v err=%v", ok, err)
	}
	if ok, err := engine.CheckRateLimit(context.Background(), "tenant-a", "route"); err != nil || ok {
		t.Fatalf("second request should be rejected: ok=%v err=%v", ok, err)
	}
}

// 作用: 验证 Test Policy_ Rate Limit_ Within Limit 场景的行为。
func TestPolicy_RateLimit_WithinLimit(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetRateLimit("tenant-a", "route", policy.RateLimit{Limit: 2, Window: time.Second})

	for i := 0; i < 2; i++ {
		if ok, err := engine.CheckRateLimit(context.Background(), "tenant-a", "route"); err != nil || !ok {
			t.Fatalf("request %d should pass: ok=%v err=%v", i, ok, err)
		}
	}
}

// 作用: 验证 Test Policy_ Blacklist_ Block 场景的行为。
func TestPolicy_Blacklist_Block(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetAccessList("tenant-a", policy.AccessList{Blacklist: []string{"+15550001"}})

	if ok, err := engine.CheckAccessList(context.Background(), "tenant-a", "+1 (555) 0001"); err != nil || ok {
		t.Fatalf("blacklisted number should be blocked: ok=%v err=%v", ok, err)
	}
}

// 作用: 验证 Test Policy_ Whitelist_ Allow Only 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestPolicy_Whitelist_AllowOnly(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetAccessList("tenant-a", policy.AccessList{Whitelist: []string{"+15550001"}})

	if ok, _ := engine.CheckAccessList(context.Background(), "tenant-a", "+15550001"); !ok {
		t.Fatalf("whitelisted number should pass")
	}
	if ok, _ := engine.CheckAccessList(context.Background(), "tenant-a", "+15550002"); ok {
		t.Fatalf("non-whitelisted number should fail")
	}
}

// 作用: 验证 Test Policy_ Authorize_ Role Permission 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestPolicy_Authorize_RolePermission(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetPermissions("tenant-a", []model.Permission{{Action: "calls:route", Resource: "calls"}})

	if ok, err := engine.Authorize(context.Background(), "tenant-a", "calls:route", "calls"); err != nil || !ok {
		t.Fatalf("permission should pass: ok=%v err=%v", ok, err)
	}
	if ok, err := engine.Authorize(context.Background(), "tenant-a", "recordings:read", "recordings"); err != nil || ok {
		t.Fatalf("permission should fail: ok=%v err=%v", ok, err)
	}
}

// 作用: 验证 Test Policy_ Tenant Isolation_ Cross Tenant Denied 场景的行为。
func TestPolicy_TenantIsolation_CrossTenantDenied(t *testing.T) {
	engine := policy.NewMemoryEngine()

	if err := engine.EnsureTenantResource("tenant-a", "tenant-b"); !errors.Is(err, policy.ErrTenantDenied) {
		t.Fatalf("expected tenant denied, got %v", err)
	}
}

// 作用: 验证 Test Policy_ Max Concurrent Calls 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestPolicy_MaxConcurrentCalls(t *testing.T) {
	engine := policy.NewMemoryEngine()
	engine.SetTenant(policy.TenantContext{TenantID: "tenant-a", Status: model.TenantStatusActive, MaxCalls: 1})

	if err := engine.AcquireCallSlot(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("first call slot should pass: %v", err)
	}
	if err := engine.AcquireCallSlot(context.Background(), "tenant-a"); !errors.Is(err, policy.ErrConcurrentLimitExceeded) {
		t.Fatalf("expected concurrent limit exceeded, got %v", err)
	}
	engine.ReleaseCallSlot("tenant-a")
	if err := engine.AcquireCallSlot(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("slot after release should pass: %v", err)
	}
}

