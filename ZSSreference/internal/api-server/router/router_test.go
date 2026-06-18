//  PBX节点路由策略：为新同传会话选择承载媒体节点
package router_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// 作用: 验证 Test Router_ Round Robin_ Even Distribution 场景的行为。
func TestRouter_RoundRobin_EvenDistribution(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-01", 0, 100, 100)
	registerMedia(t, reg, "media-02", 0, 100, 100)

	first := route(t, r, router.RouteStrategyRoundRobin)
	second := route(t, r, router.RouteStrategyRoundRobin)
	third := route(t, r, router.RouteStrategyRoundRobin)

	if first.MediaNode != "media-01" || second.MediaNode != "media-02" || third.MediaNode != "media-01" {
		t.Fatalf("unexpected round robin order: %s %s %s", first.MediaNode, second.MediaNode, third.MediaNode)
	}
}

// 作用: 验证 Test Router_ Weighted Round Robin_ By Weight 场景的行为。
func TestRouter_WeightedRoundRobin_ByWeight(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-low", 0, 100, 1)
	registerMedia(t, reg, "media-high", 0, 100, 10)

	result := route(t, r, router.RouteStrategyWeightedRoundRobin)
	if result.MediaNode != "media-high" {
		t.Fatalf("expected high weight node, got %s", result.MediaNode)
	}
}

// 作用: 验证 Test Router_ Least Connections 场景的行为。
func TestRouter_LeastConnections(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-busy", 50, 100, 100)
	registerMedia(t, reg, "media-idle", 2, 100, 100)

	result := route(t, r, router.RouteStrategyLeastConnections)
	if result.MediaNode != "media-idle" {
		t.Fatalf("expected idle node, got %s", result.MediaNode)
	}
}

// 作用: 验证 Test Router_ Exclude Down And Draining 场景的行为。
func TestRouter_ExcludeDownAndDraining(t *testing.T) {
	r, reg := newRouter()
	registerMediaWithStatus(t, reg, "media-down", model.NodeStatusDown)
	registerMediaWithStatus(t, reg, "media-draining", model.NodeStatusDraining)
	registerMediaWithStatus(t, reg, "media-up", model.NodeStatusUp)

	result := route(t, r, router.RouteStrategyRoundRobin)
	if result.MediaNode != "media-up" {
		t.Fatalf("expected up node, got %s", result.MediaNode)
	}
}

// 作用: 验证 Test Router_ No Available Node 场景的行为。
func TestRouter_NoAvailableNode(t *testing.T) {
	r, reg := newRouter()
	registerMediaWithStatus(t, reg, "media-down", model.NodeStatusDown)

	_, err := r.Route(context.Background(), router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002"})
	if !errors.Is(err, router.ErrNoAvailableNode) {
		t.Fatalf("expected no available node, got %v", err)
	}
}

// 作用: 验证 Test Router_ Internal Extension Route 场景的行为。
func TestRouter_InternalExtensionRoute(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-01", 0, 100, 100)

	result := route(t, r, router.RouteStrategyRoundRobin)
	if result.RouteType != router.RouteTypeInternal {
		t.Fatalf("expected internal route, got %s", result.RouteType)
	}
}

// 作用: 验证 Test Router_ Trunk Outbound Route 场景的行为。
func TestRouter_TrunkOutboundRoute(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-01", 0, 100, 100)

	result, err := r.Route(context.Background(), router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "+15550001"})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.RouteType != router.RouteTypeOutbound {
		t.Fatalf("expected outbound route, got %s", result.RouteType)
	}
}

// 作用: 验证 Test Router_ A I Pipeline_ Match Language Model 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRouter_AIPipeline_MatchLanguageModel(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-01", 0, 100, 100)
	registerCapability(t, reg, model.Capability{ID: "vad-01", Type: model.CapabilityTypeVAD, Protocol: "grpc", MaxConcurrency: 10})
	registerCapability(t, reg, model.Capability{ID: "asr-01", Type: model.CapabilityTypeASR, Protocol: "grpc", Languages: []string{"zh-CN"}, Models: []string{"paraformer"}, MaxConcurrency: 10})
	registerCapability(t, reg, model.Capability{ID: "tts-01", Type: model.CapabilityTypeTTS, Protocol: "grpc", Languages: []string{"zh-CN"}, MaxConcurrency: 10})

	result, err := r.Route(context.Background(), router.RouteRequest{
		TenantID: "tenant-a",
		Caller:   "1001",
		Callee:   "1002",
		NeedAI:   true,
		Language: "zh-CN",
		ASRModel: "paraformer",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result.AIPipeline == nil || result.AIPipeline.ASR != "asr-01" || result.AIPipeline.TTS != "tts-01" || result.AIPipeline.Agent != "" {
		t.Fatalf("unexpected ai pipeline: %#v", result.AIPipeline)
	}
}

// 作用: 验证 Test Router_ Call I D Unique 场景的行为。
func TestRouter_CallIDUnique(t *testing.T) {
	r, reg := newRouter()
	registerMedia(t, reg, "media-01", 0, 100, 100)

	first := route(t, r, router.RouteStrategyRoundRobin)
	second := route(t, r, router.RouteStrategyRoundRobin)
	if first.CallID == second.CallID {
		t.Fatalf("expected unique call ids")
	}
}

// 作用: 处理 new Router 的核心流程。
func newRouter() (*router.Router, *registry.Registry) {
	reg := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	return router.New(reg), reg
}

// 作用: 处理 route 的核心流程。
func route(t *testing.T, r *router.Router, strategy router.RouteStrategy) router.RouteResult {
	t.Helper()
	result, err := r.Route(context.Background(), router.RouteRequest{
		TenantID: "tenant-a",
		Caller:   "1001",
		Callee:   "1002",
		Strategy: strategy,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	return result
}

// 作用: 处理 register Media 的核心流程。
func registerMedia(t *testing.T, reg *registry.Registry, id string, currentCalls, maxCalls, weight int) {
	t.Helper()
	if err := reg.Register(context.Background(), &model.Node{
		ID:           id,
		Type:         model.NodeTypeMedia,
		Endpoint:     "127.0.0.1:8021",
		Status:       model.NodeStatusUp,
		Weight:       weight,
		MaxCalls:     maxCalls,
		CurrentCalls: currentCalls,
	}); err != nil {
		t.Fatalf("register media: %v", err)
	}
}

// 作用: 处理 register Media With Status 的核心流程。
func registerMediaWithStatus(t *testing.T, reg *registry.Registry, id string, status model.NodeStatus) {
	t.Helper()
	if err := reg.Register(context.Background(), &model.Node{
		ID:       id,
		Type:     model.NodeTypeMedia,
		Endpoint: "127.0.0.1:8021",
		Status:   status,
		Weight:   100,
		MaxCalls: 100,
	}); err != nil {
		t.Fatalf("register media: %v", err)
	}
}

// 作用: 处理 register Capability 的核心流程。
func registerCapability(t *testing.T, reg *registry.Registry, capability model.Capability) {
	t.Helper()
	if err := reg.RegisterCapability(context.Background(), capability); err != nil {
		t.Fatalf("register capability: %v", err)
	}
}

