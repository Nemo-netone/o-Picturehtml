//  ICE候选处理：NAT穿透候选收集与优选
package ice_test

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/ice"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// 作用: 验证 Test I C E_ Select Healthy Turn Node 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestICE_SelectHealthyTurnNode(t *testing.T) {
	service, reg := newService()
	registerTurn(t, reg, "turn-01", model.NodeStatusUp, "az-a")
	registerTurn(t, reg, "turn-02", model.NodeStatusDown, "az-a")

	cfg, err := service.GetICEConfig(context.Background(), ice.Request{TenantID: "tenant-a", Zone: "az-a"})
	if err != nil {
		t.Fatalf("get ice config: %v", err)
	}
	if len(cfg.TURNServers) != 1 || cfg.TURNServers[0].NodeID != "turn-01" {
		t.Fatalf("unexpected turn servers: %#v", cfg.TURNServers)
	}
}

// 作用: 验证 Test I C E_ Ephemeral Credential_ Valid 场景的行为。
func TestICE_EphemeralCredential_Valid(t *testing.T) {
	service, _ := newService()
	cred := service.GenerateCredential("tenant-a", time.Minute)

	if !service.ValidateCredential(cred, time.Now()) {
		t.Fatalf("expected credential to be valid")
	}
}

// 作用: 验证 Test I C E_ Ephemeral Credential_ Expired 场景的行为。
func TestICE_EphemeralCredential_Expired(t *testing.T) {
	service, _ := newService()
	cred := service.GenerateCredential("tenant-a", time.Second)

	if service.ValidateCredential(cred, time.Now().Add(2*time.Second)) {
		t.Fatalf("expected credential to expire")
	}
}

// 作用: 验证 Test I C E_ Tenant Zone Preference 场景的行为。
func TestICE_TenantZonePreference(t *testing.T) {
	service, reg := newService()
	registerTurn(t, reg, "turn-a", model.NodeStatusUp, "az-a")
	registerTurn(t, reg, "turn-b", model.NodeStatusUp, "az-b")

	cfg, err := service.GetICEConfig(context.Background(), ice.Request{TenantID: "tenant-a", Zone: "az-b"})
	if err != nil {
		t.Fatalf("get ice config: %v", err)
	}
	if cfg.TURNServers[0].NodeID != "turn-b" {
		t.Fatalf("expected zone b first, got %#v", cfg.TURNServers)
	}
}

// 作用: 验证 Test I C E_ No Turn Fallback To Stun 场景的行为。
func TestICE_NoTurnFallbackToStun(t *testing.T) {
	service, _ := newService()
	cfg, err := service.GetICEConfig(context.Background(), ice.Request{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("get ice config: %v", err)
	}
	if len(cfg.TURNServers) != 0 || len(cfg.STUNServers) == 0 {
		t.Fatalf("expected stun fallback, got %#v", cfg)
	}
}

// 作用: 处理 new Service 的核心流程。
func newService() (*ice.Service, *registry.Registry) {
	reg := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	return ice.New(reg, ice.Options{SharedSecret: "secret", DefaultSTUN: []string{"stun:stun.l.google.com:19302"}}), reg
}

// 作用: 处理 register Turn 的核心流程。
func registerTurn(t *testing.T, reg *registry.Registry, id string, status model.NodeStatus, zone string) {
	t.Helper()
	if err := reg.Register(context.Background(), &model.Node{
		ID:       id,
		Type:     model.NodeTypeTurn,
		Endpoint: "turn.example.com:3478",
		Status:   status,
		Zone:     zone,
	}); err != nil {
		t.Fatalf("register turn: %v", err)
	}
}

