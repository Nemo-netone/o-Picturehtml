//  etcd配置中心：运行时动态配置读写
package configcenter

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

const (
	configRoot         = "/pbx/config"
	ResourceTenants    = "tenants"
	ResourceExtensions = "extensions"
	ResourceTrunks     = "trunks"
	ResourceRoutes     = "routes"
	ResourceAIPolicies = "ai-policies"
	ResourceICE        = "ice"
)

var (
	ErrConfigNotFound  = errors.New("config not found")
	ErrVersionConflict = errors.New("config version conflict")
	ErrInvalidConfig   = errors.New("invalid config")
)

type ConfigEventType string

const (
	ConfigEventPut    ConfigEventType = "put"
	ConfigEventDelete ConfigEventType = "delete"
)

type ConfigEvent struct {
	Type     ConfigEventType
	TenantID string
	Resource string
	Key      string
	Version  int64
	Value    []byte
}

type Store struct {
	client etcdutil.Client
}

// New 创建配置中心存储实例。
func New(client etcdutil.Client) *Store {
	return &Store{client: client}
}

// SetTenant 写入租户配置（带版本号，乐观并发控制）。
func (s *Store) SetTenant(ctx context.Context, tenant model.Tenant) error {
	if tenant.ID == "" {
		return fmt.Errorf("tenant id: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, tenantKey(tenant.ID), &tenant)
}

// GetTenant 读取租户配置。
func (s *Store) GetTenant(ctx context.Context, tenantID string) (*model.Tenant, error) {
	var tenant model.Tenant
	if err := s.get(ctx, tenantKey(tenantID), &tenant); err != nil {
		return nil, err
	}
	return &tenant, nil
}

// SetExtension 写入分机配置。
func (s *Store) SetExtension(ctx context.Context, extension model.Extension) error {
	if extension.TenantID == "" || extension.Extension == "" {
		return fmt.Errorf("extension tenant and number: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, extensionKey(extension.TenantID, extension.Extension), &extension)
}

// GetExtension 读取分机配置。
func (s *Store) GetExtension(ctx context.Context, tenantID, extension string) (*model.Extension, error) {
	var ext model.Extension
	if err := s.get(ctx, extensionKey(tenantID, extension), &ext); err != nil {
		return nil, err
	}
	return &ext, nil
}

// DeleteExtension 删除分机配置。
func (s *Store) DeleteExtension(ctx context.Context, tenantID, extension string) error {
	return s.delete(ctx, extensionKey(tenantID, extension))
}

// ListExtensions 列出某租户的所有分机。
func (s *Store) ListExtensions(ctx context.Context, tenantID string) ([]model.Extension, error) {
	items, err := s.client.GetPrefix(ctx, resourceTenantPrefix(ResourceExtensions, tenantID))
	if err != nil {
		return nil, err
	}
	extensions := make([]model.Extension, 0, len(items))
	for _, item := range items {
		var ext model.Extension
		if err := etcdutil.Unmarshal(item.Value, &ext); err != nil {
			return nil, err
		}
		extensions = append(extensions, ext)
	}
	// 按分机号排序保证输出稳定。
	sort.Slice(extensions, func(i, j int) bool {
		return extensions[i].Extension < extensions[j].Extension
	})
	return extensions, nil
}

// SetTrunk 写入 SIP 中继配置。
func (s *Store) SetTrunk(ctx context.Context, trunk model.SIPTrunk) error {
	if trunk.TenantID == "" || trunk.ID == "" {
		return fmt.Errorf("trunk tenant and id: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, trunkKey(trunk.TenantID, trunk.ID), &trunk)
}

// GetTrunk 读取 SIP 中继配置。
func (s *Store) GetTrunk(ctx context.Context, tenantID, trunkID string) (*model.SIPTrunk, error) {
	var trunk model.SIPTrunk
	if err := s.get(ctx, trunkKey(tenantID, trunkID), &trunk); err != nil {
		return nil, err
	}
	return &trunk, nil
}

// DeleteTrunk 删除 SIP 中继配置。
func (s *Store) DeleteTrunk(ctx context.Context, tenantID, trunkID string) error {
	return s.delete(ctx, trunkKey(tenantID, trunkID))
}

// SetRoute 写入路由规则。
func (s *Store) SetRoute(ctx context.Context, route model.Route) error {
	if route.TenantID == "" || route.ID == "" {
		return fmt.Errorf("route tenant and id: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, routeKey(route.TenantID, route.ID), &route)
}

// GetRoute 读取路由规则。
func (s *Store) GetRoute(ctx context.Context, tenantID, routeID string) (*model.Route, error) {
	var route model.Route
	if err := s.get(ctx, routeKey(tenantID, routeID), &route); err != nil {
		return nil, err
	}
	return &route, nil
}

// DeleteRoute 删除路由规则。
func (s *Store) DeleteRoute(ctx context.Context, tenantID, routeID string) error {
	return s.delete(ctx, routeKey(tenantID, routeID))
}

// ListRoutes 列出某租户的所有路由规则，按优先级+ID 排序。
func (s *Store) ListRoutes(ctx context.Context, tenantID string) ([]model.Route, error) {
	items, err := s.client.GetPrefix(ctx, resourceTenantPrefix(ResourceRoutes, tenantID))
	if err != nil {
		return nil, err
	}
	routes := make([]model.Route, 0, len(items))
	for _, item := range items {
		var route model.Route
		if err := etcdutil.Unmarshal(item.Value, &route); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	// 先按优先级后按 ID 排序，保证路由匹配顺序确定。
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Priority == routes[j].Priority {
			return routes[i].ID < routes[j].ID
		}
		return routes[i].Priority < routes[j].Priority
	})
	return routes, nil
}

// SetAIPolicy 写入 AI 策略配置（prompt、TTS 设置等）。
func (s *Store) SetAIPolicy(ctx context.Context, policy model.AIPolicy) error {
	if policy.TenantID == "" || policy.ID == "" || policy.Language == "" {
		return fmt.Errorf("ai policy tenant, id and language: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, aiPolicyKey(policy.TenantID, policy.ID), &policy)
}

// GetAIPolicy 读取 AI 策略配置。
func (s *Store) GetAIPolicy(ctx context.Context, tenantID, policyID string) (*model.AIPolicy, error) {
	var policy model.AIPolicy
	if err := s.get(ctx, aiPolicyKey(tenantID, policyID), &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

// SetICEConfig 写入 ICE（STUN/TURN）配置。
func (s *Store) SetICEConfig(ctx context.Context, cfg model.ICEConfig) error {
	if cfg.TenantID == "" {
		return fmt.Errorf("ice tenant: %w", ErrInvalidConfig)
	}
	return s.putVersioned(ctx, iceKey(cfg.TenantID), &cfg)
}

// GetICEConfig 读取 ICE（STUN/TURN）配置。
func (s *Store) GetICEConfig(ctx context.Context, tenantID string) (*model.ICEConfig, error) {
	var cfg model.ICEConfig
	if err := s.get(ctx, iceKey(tenantID), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// WatchConfig 监听某租户某资源的配置变更，返回带缓冲的 ConfigEvent channel。
func (s *Store) WatchConfig(ctx context.Context, tenantID, resource string) <-chan ConfigEvent {
	events := s.client.WatchPrefix(ctx, resourceTenantPrefix(resource, tenantID))
	out := make(chan ConfigEvent, 16)

	// 后台协程将 etcd 事件映射为 ConfigEvent（put/delete）。
	go func() {
		defer close(out)
		for event := range events {
			typ := ConfigEventPut
			if event.Type == etcdutil.EventDelete {
				typ = ConfigEventDelete
			}
			cfgEvent := ConfigEvent{
				Type:     typ,
				TenantID: tenantID,
				Resource: resource,
				Key:      event.Key,
				Version:  event.Version,
				Value:    event.Value,
			}
			select {
			case out <- cfgEvent:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

// putVersioned 带版本号写入配置：key 不存在时创建（版本=1），存在时校验版本冲突后递增写入。
func (s *Store) putVersioned(ctx context.Context, key string, value versionedModel) error {
	existing, err := s.client.Get(ctx, key)
	if errors.Is(err, etcdutil.ErrKeyNotFound) {
		if value.GetMetadata().Version > 1 {
			return ErrVersionConflict
		}
		value.GetMetadata().Version = 1
		data, err := etcdutil.Marshal(value)
		if err != nil {
			return err
		}
		_, err = s.client.Put(ctx, key, data)
		return err
	}
	if err != nil {
		return err
	}

	var current metadataOnly
	if err := etcdutil.Unmarshal(existing.Value, &current); err != nil {
		return err
	}
	currentVersion := current.normalizedVersion()
	if value.GetMetadata().Version != 0 && value.GetMetadata().Version != currentVersion {
		return ErrVersionConflict
	}
	value.GetMetadata().Version = currentVersion + 1
	data, err := etcdutil.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.UpdateIfVersion(ctx, key, existing.Version, data)
}

// get 从 etcd 读取 key 并反序列化到 target，key 不存在返回 ErrConfigNotFound。
func (s *Store) get(ctx context.Context, key string, target any) error {
	item, err := s.client.Get(ctx, key)
	if errors.Is(err, etcdutil.ErrKeyNotFound) {
		return ErrConfigNotFound
	}
	if err != nil {
		return err
	}
	return etcdutil.Unmarshal(item.Value, target)
}

// delete 删除配置 key，不存在时返回 ErrConfigNotFound。
func (s *Store) delete(ctx context.Context, key string) error {
	if err := s.client.Delete(ctx, key); errors.Is(err, etcdutil.ErrKeyNotFound) {
		return ErrConfigNotFound
	} else if err != nil {
		return err
	}
	return nil
}

type versionedModel interface {
	GetMetadata() *model.Metadata
}

type metadataOnly struct {
	Metadata model.Metadata `json:"metadata,omitempty"`
	Version  int64          `json:"version,omitempty"`
}

// normalizedVersion 从 Metadata 或 Version 字段读取版本号（兼容两种序列化格式）。
func (m *metadataOnly) normalizedVersion() int64 {
	if m.Metadata.Version != 0 {
		return m.Metadata.Version
	}
	return m.Version
}

// tenantKey 构建 etcd 键：/pbx/config/tenants/{id}。
func tenantKey(tenantID string) string {
	return etcdutil.JoinKey(configRoot, ResourceTenants, tenantID)
}

// extensionKey 构建 etcd 键：/pbx/config/extensions/{tenant}/{ext}。
func extensionKey(tenantID, extension string) string {
	return etcdutil.JoinKey(configRoot, ResourceExtensions, tenantID, extension)
}

// trunkKey 构建 etcd 键：/pbx/config/trunks/{tenant}/{trunk}。
func trunkKey(tenantID, trunkID string) string {
	return etcdutil.JoinKey(configRoot, ResourceTrunks, tenantID, trunkID)
}

// routeKey 构建 etcd 键：/pbx/config/routes/{tenant}/{route}。
func routeKey(tenantID, routeID string) string {
	return etcdutil.JoinKey(configRoot, ResourceRoutes, tenantID, routeID)
}

// aiPolicyKey 构建 etcd 键：/pbx/config/ai-policies/{tenant}/{policy}。
func aiPolicyKey(tenantID, policyID string) string {
	return etcdutil.JoinKey(configRoot, ResourceAIPolicies, tenantID, policyID)
}

// iceKey 构建 etcd 键：/pbx/config/ice/{tenant}/default。
func iceKey(tenantID string) string {
	return etcdutil.JoinKey(configRoot, ResourceICE, tenantID, "default")
}

// resourceTenantPrefix 构建某租户资源的 etcd 前缀：/pbx/config/{resource}/{tenant}/。
func resourceTenantPrefix(resource, tenantID string) string {
	return etcdutil.JoinKey(configRoot, resource, tenantID) + "/"
}

