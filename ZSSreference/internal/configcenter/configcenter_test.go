//  etcd配置中心：运行时动态配置读写
package configcenter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test Config Store_ Extension C R U D 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_ExtensionCRUD(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())

	ext := testExtension("tenant-a", "1001")
	if err := store.SetExtension(ctx, ext); err != nil {
		t.Fatalf("set extension: %v", err)
	}

	got, err := store.GetExtension(ctx, "tenant-a", "1001")
	if err != nil {
		t.Fatalf("get extension: %v", err)
	}
	if got.Extension != "1001" || got.Metadata.Version != 1 {
		t.Fatalf("unexpected extension: %#v", got)
	}

	if err := store.DeleteExtension(ctx, "tenant-a", "1001"); err != nil {
		t.Fatalf("delete extension: %v", err)
	}
	if _, err := store.GetExtension(ctx, "tenant-a", "1001"); err == nil {
		t.Fatalf("expected deleted extension to be missing")
	}
}

// 作用: 验证 Test Config Store_ Trunk C R U D 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_TrunkCRUD(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())

	trunk := model.SIPTrunk{ID: "trunk-1", TenantID: "tenant-a", Provider: "carrier", Endpoint: "sip.example.com", Transport: "udp", Status: model.SIPTrunkStatusEnabled}
	if err := store.SetTrunk(ctx, trunk); err != nil {
		t.Fatalf("set trunk: %v", err)
	}
	got, err := store.GetTrunk(ctx, "tenant-a", "trunk-1")
	if err != nil {
		t.Fatalf("get trunk: %v", err)
	}
	if got.Endpoint != trunk.Endpoint {
		t.Fatalf("unexpected trunk: %#v", got)
	}
}

// 作用: 验证 Test Config Store_ Route Priority Sort 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_RoutePrioritySort(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())

	_ = store.SetRoute(ctx, model.Route{ID: "r2", TenantID: "tenant-a", Direction: model.RouteDirectionOutbound, Priority: 20, Enabled: true})
	_ = store.SetRoute(ctx, model.Route{ID: "r1", TenantID: "tenant-a", Direction: model.RouteDirectionOutbound, Priority: 10, Enabled: true})

	routes, err := store.ListRoutes(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 2 || routes[0].ID != "r1" || routes[1].ID != "r2" {
		t.Fatalf("routes not sorted by priority: %#v", routes)
	}
}

// 作用: 验证 Test Config Store_ A I Policy Validation 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_AIPolicyValidation(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())

	policy := model.AIPolicy{ID: "ai-1", TenantID: "tenant-a", Enabled: true}
	if err := store.SetAIPolicy(ctx, policy); !errors.Is(err, configcenter.ErrInvalidConfig) {
		t.Fatalf("expected invalid policy, got %v", err)
	}

	policy.Language = "zh-CN"
	if err := store.SetAIPolicy(ctx, policy); err != nil {
		t.Fatalf("set valid policy: %v", err)
	}
}

// 作用: 验证 Test Config Store_ I C E Config C R U D 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_ICEConfigCRUD(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())
	cfg := model.ICEConfig{
		TenantID:      "tenant-a",
		STUNServers:   []model.ICEServer{{URLs: []string{"stun:stun.example.com:3478"}}},
		CredentialTTL: 3600,
	}

	if err := store.SetICEConfig(ctx, cfg); err != nil {
		t.Fatalf("set ice config: %v", err)
	}
	got, err := store.GetICEConfig(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get ice config: %v", err)
	}
	if len(got.STUNServers) != 1 || got.STUNServers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Fatalf("unexpected ice config: %#v", got)
	}
}

// 作用: 验证 Test Config Store_ C A S Version Conflict 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_CASVersionConflict(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())
	ext := testExtension("tenant-a", "1001")

	if err := store.SetExtension(ctx, ext); err != nil {
		t.Fatalf("set extension: %v", err)
	}
	ext.Metadata.Version = 99
	if err := store.SetExtension(ctx, ext); !errors.Is(err, configcenter.ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

// 作用: 验证 Test Config Store_ Watch Put Delete 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfigStore_WatchPutDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := configcenter.New(etcdutil.NewMemoryClient())

	events := store.WatchConfig(ctx, "tenant-a", configcenter.ResourceExtensions)
	_ = store.SetExtension(ctx, testExtension("tenant-a", "1001"))
	_ = store.DeleteExtension(ctx, "tenant-a", "1001")

	first := mustReadConfigEvent(t, events)
	if first.Type != configcenter.ConfigEventPut || first.TenantID != "tenant-a" {
		t.Fatalf("unexpected first event: %#v", first)
	}
	second := mustReadConfigEvent(t, events)
	if second.Type != configcenter.ConfigEventDelete {
		t.Fatalf("unexpected second event: %#v", second)
	}
}

// 作用: 验证 Test Config Store_ Tenant Isolation 场景的行为。
func TestConfigStore_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := configcenter.New(etcdutil.NewMemoryClient())
	_ = store.SetExtension(ctx, testExtension("tenant-a", "1001"))

	if _, err := store.GetExtension(ctx, "tenant-b", "1001"); err == nil {
		t.Fatalf("expected tenant-b to be unable to read tenant-a extension")
	}
}

// 作用: 处理 test Extension 的核心流程。
func testExtension(tenantID, extension string) model.Extension {
	return model.Extension{
		ID:          tenantID + "-" + extension,
		TenantID:    tenantID,
		Extension:   extension,
		DisplayName: "Alice",
		Status:      model.ExtensionStatusEnabled,
	}
}

// 作用: 读取测试所需的 must Read Config Event，失败时终止测试。
func mustReadConfigEvent(t *testing.T, ch <-chan configcenter.ConfigEvent) configcenter.ConfigEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config event")
	}
	return configcenter.ConfigEvent{}
}

