//  端到端测试：完整同传流程验证
package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/gateway"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/cdr"
	"github.com/SATA260/SimulSpeak1/internal/pbx/media"
	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
	"github.com/SATA260/SimulSpeak1/internal/pbx/transcription"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
)

// 作用: 验证 Test E2 E_ Node Register Fault Remove 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_NodeRegisterFaultRemove(t *testing.T) {
	client := etcdutil.NewMemoryClient()
	reg := registry.New(client, registry.Options{LeaseTTL: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())

	if err := reg.Register(ctx, mediaNode("media-1")); err != nil {
		t.Fatalf("register: %v", err)
	}
	cancel()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := reg.GetNode(context.Background(), model.NodeTypeMedia, "media-1"); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for lease removal")
}

// 作用: 验证 Test E2 E_ Web R T C Internal Call 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_WebRTCInternalCall(t *testing.T) {
	ctx := context.Background()
	configs := configcenter.New(etcdutil.NewMemoryClient())
	_ = configs.SetTenant(ctx, model.Tenant{ID: "tenant-a", Name: "Tenant A", Tier: model.TenantTierPro, Status: model.TenantStatusActive})
	_ = configs.SetExtension(ctx, extension("tenant-a", "1001"))
	_ = configs.SetExtension(ctx, extension("tenant-a", "1002"))

	gw := gateway.New(gateway.Options{MaxConnections: 10, HeartbeatTimeout: time.Minute})
	caller, err := gw.Connect(ctx, "tenant-a:1001")
	if err != nil {
		t.Fatalf("connect caller: %v", err)
	}
	callee, err := gw.Connect(ctx, "tenant-a:1002")
	if err != nil {
		t.Fatalf("connect callee: %v", err)
	}
	if caller.ID == callee.ID {
		t.Fatalf("expected distinct websocket sessions")
	}

	adapter := media.NewMockAdapter("media-1")
	records := cdr.NewService()
	started := time.Now().UTC()
	_, _ = adapter.Originate(ctx, media.OriginateRequest{CallID: "call-1", Caller: "1001", Callee: "1002"})
	_ = records.Create(ctx, cdr.Record{TenantID: "tenant-a", CallID: "call-1", Caller: "1001", Callee: "1002", StartedAt: started})
	_ = adapter.Bridge(ctx, "call-1")
	_, _ = records.MarkAnswered(ctx, "tenant-a", "call-1", started.Add(time.Second))
	_ = adapter.Hangup(ctx, "call-1")
	record, err := records.End(ctx, "tenant-a", "call-1", started.Add(31*time.Second))
	if err != nil {
		t.Fatalf("end cdr: %v", err)
	}
	if record.Duration != 30*time.Second {
		t.Fatalf("unexpected cdr duration: %s", record.Duration)
	}
	call, _ := adapter.GetCall("call-1")
	if call.State != media.CallStateHungup {
		t.Fatalf("expected hungup, got %#v", call)
	}
}

// 作用: 验证 Test E2 E_ Trunk Inbound Outbound C D R 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_TrunkInboundOutboundCDR(t *testing.T) {
	ctx := context.Background()
	configs := configcenter.New(etcdutil.NewMemoryClient())
	trunk := model.SIPTrunk{
		ID:        "trunk-1",
		TenantID:  "tenant-a",
		Provider:  "carrier",
		Endpoint:  "sip.carrier.example",
		Transport: "udp",
		Status:    model.SIPTrunkStatusEnabled,
		InboundRules: []model.InboundRule{{
			ID:      "did-1",
			DID:     "+15551230000",
			RouteID: "inbound-ai",
			Enabled: true,
		}},
		OutboundRules: []model.OutboundRule{{ID: "out-1", Prefix: "+1", Priority: 10, Enabled: true}},
	}
	if err := configs.SetTrunk(ctx, trunk); err != nil {
		t.Fatalf("set trunk: %v", err)
	}
	if err := configs.SetRoute(ctx, model.Route{ID: "inbound-ai", TenantID: "tenant-a", Direction: model.RouteDirectionInbound, Priority: 10, Enabled: true, Actions: []model.RouteAction{{Type: model.RouteActionAI, TargetID: "ai-policy-1"}}}); err != nil {
		t.Fatalf("set inbound route: %v", err)
	}
	if err := configs.SetRoute(ctx, model.Route{ID: "outbound-trunk", TenantID: "tenant-a", Direction: model.RouteDirectionOutbound, Priority: 10, Enabled: true, Actions: []model.RouteAction{{Type: model.RouteActionTrunk, TargetID: "trunk-1"}}}); err != nil {
		t.Fatalf("set outbound route: %v", err)
	}
	gotTrunk, _ := configs.GetTrunk(ctx, "tenant-a", "trunk-1")
	gotInbound, _ := configs.GetRoute(ctx, "tenant-a", gotTrunk.InboundRules[0].RouteID)
	if gotInbound.Actions[0].Type != model.RouteActionAI {
		t.Fatalf("expected DID to route to AI: %#v", gotInbound)
	}

	records := cdr.NewService()
	started := time.Now()
	_ = records.Create(ctx, cdr.Record{TenantID: "tenant-a", CallID: "outbound-1", Caller: "1001", Callee: "+15551230000", StartedAt: started})
	_, _ = records.MarkAnswered(ctx, "tenant-a", "outbound-1", started.Add(time.Second))
	ended, err := records.End(ctx, "tenant-a", "outbound-1", started.Add(11*time.Second))
	if err != nil {
		t.Fatalf("end outbound cdr: %v", err)
	}
	if ended.Duration != 10*time.Second {
		t.Fatalf("unexpected outbound duration: %s", ended.Duration)
	}
}

// 作用: 验证 Test E2 E_ Node Drain Failure And Provider Fallback 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_NodeDrainFailureAndProviderFallback(t *testing.T) {
	ctx := context.Background()
	reg := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	draining := mediaNode("media-draining")
	draining.Status = model.NodeStatusDraining
	healthy := mediaNode("media-healthy")
	_ = reg.Register(ctx, draining)
	_ = reg.Register(ctx, healthy)
	_ = reg.RegisterCapability(ctx, vadCapability("vad-1"))
	_ = reg.RegisterCapability(ctx, asrCapability("asr-1"))
	_ = reg.RegisterCapability(ctx, ttsCapability("tts-1"))
	result, err := router.New(reg).Route(ctx, router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002", NeedAI: true, Language: "en-US"})
	if err != nil {
		t.Fatalf("route with healthy node: %v", err)
	}
	if result.MediaNode != "media-healthy" {
		t.Fatalf("new call should avoid draining node: %#v", result)
	}

	manager := session.New(etcdutil.NewMemoryClient())
	call, err := manager.CreateSession(ctx, "call-1", session.CreateRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002", OwnerNode: "gw-1", GatewayNode: "gw-1", MediaNode: "media-healthy"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	connected := model.CallStateConnected
	_ = manager.UpdateSession(ctx, call.ID, call.Owner.Epoch, session.Update{State: &connected})
	if err := manager.MarkSuspect(ctx, call.ID); err != nil {
		t.Fatalf("mark suspect: %v", err)
	}
	if err := manager.MarkLost(ctx, call.ID); err != nil {
		t.Fatalf("mark lost: %v", err)
	}
	lost, _ := manager.GetSession(ctx, call.ID)
	if lost.State != model.CallStateLost {
		t.Fatalf("expected lost session: %#v", lost)
	}
}

// 作用: 验证 Test E2 E_ Config Hot Update 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_ConfigHotUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := configcenter.New(etcdutil.NewMemoryClient())
	route := model.Route{ID: "route-1", TenantID: "tenant-a", Direction: model.RouteDirectionInternal, Priority: 10, Enabled: true, Actions: []model.RouteAction{{Type: model.RouteActionExtension, TargetID: "1001"}}}
	if err := store.SetRoute(ctx, route); err != nil {
		t.Fatalf("set route: %v", err)
	}
	watch := store.WatchConfig(ctx, "tenant-a", configcenter.ResourceRoutes)
	got, _ := store.GetRoute(ctx, "tenant-a", "route-1")
	got.Actions[0].TargetID = "1002"
	if err := store.SetRoute(ctx, *got); err != nil {
		t.Fatalf("hot update route: %v", err)
	}
	event := mustConfigEvent(t, watch)
	if event.Type != configcenter.ConfigEventPut || event.Version == 0 {
		t.Fatalf("unexpected config event: %#v", event)
	}
	updated, _ := store.GetRoute(ctx, "tenant-a", "route-1")
	if updated.Actions[0].TargetID != "1002" || updated.Metadata.Version <= route.Metadata.Version {
		t.Fatalf("route was not updated: %#v", updated)
	}
}

// 作用: 验证 Test E2 E_ Tenant Isolation 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestE2E_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	configs := configcenter.New(etcdutil.NewMemoryClient())
	_ = configs.SetExtension(ctx, extension("tenant-a", "1001"))
	if _, err := configs.GetExtension(ctx, "tenant-b", "1001"); err == nil {
		t.Fatalf("expected config tenant isolation")
	}

	recordings := recording.NewService(storage.NewMemoryStorage())
	_, _ = recordings.Start(ctx, "tenant-a", "call-1", "rec-1", time.Now())
	if _, err := recordings.Get(ctx, "tenant-b", "rec-1"); !errors.Is(err, recording.ErrAccessDenied) {
		t.Fatalf("expected recording access denied, got %v", err)
	}

	transcripts := transcription.NewService()
	_ = transcripts.Write(ctx, "tenant-a", model.ASRResult{CallID: "call-1", Text: "tenant a only", IsFinal: true})
	got, err := transcripts.SearchByCall(ctx, "tenant-b", "call-1")
	if err != nil {
		t.Fatalf("search transcript: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected transcription tenant isolation, got %#v", got)
	}
}

// 作用: 压测 Benchmark Route A P I 场景的性能基线。
// 逻辑: 先准备压测上下文，再循环执行目标操作，最后由 testing 框架统计指标。
func BenchmarkRouteAPI(b *testing.B) {
	ctx := context.Background()
	reg := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	_ = reg.Register(ctx, mediaNode("media-1"))
	_ = reg.RegisterCapability(ctx, vadCapability("vad-1"))
	_ = reg.RegisterCapability(ctx, asrCapability("asr-1"))
	_ = reg.RegisterCapability(ctx, ttsCapability("tts-1"))
	rt := router.New(reg)
	req := router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002", NeedAI: true, Language: "en-US", Strategy: router.RouteStrategyRoundRobin}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := rt.Route(ctx, req); err != nil {
			b.Fatalf("route: %v", err)
		}
	}
}

// 作用: 压测 Benchmark Web Socket Connections 场景的性能基线。
func BenchmarkWebSocketConnections(b *testing.B) {
	ctx := context.Background()
	gw := gateway.New(gateway.Options{MaxConnections: 1_000_000, HeartbeatTimeout: time.Minute})

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := gw.Connect(ctx, "tenant-a:"+benchExtension(i)); err != nil {
			b.Fatalf("connect: %v", err)
		}
	}
}

type testFataler interface {
	Fatalf(format string, args ...any)
	Helper()
}

// 作用: 处理 media Node 的核心流程。
func mediaNode(id string) *model.Node {
	return &model.Node{ID: id, Type: model.NodeTypeMedia, Endpoint: "127.0.0.1:8021", Zone: "az-a", Status: model.NodeStatusUp, Weight: 100, MaxCalls: 1000}
}

// 作用: 处理 extension 的核心流程。
func extension(tenantID, number string) model.Extension {
	return model.Extension{ID: tenantID + "-" + number, TenantID: tenantID, Extension: number, DisplayName: number, Status: model.ExtensionStatusEnabled}
}

// 作用: 处理 vad Capability 的核心流程。
func vadCapability(id string) model.Capability {
	return model.Capability{ID: id, Type: model.CapabilityTypeVAD, Protocol: "grpc", Streaming: true, MaxConcurrency: 1000}
}

// 作用: 处理 asr Capability 的核心流程。
func asrCapability(id string) model.Capability {
	return model.Capability{ID: id, Type: model.CapabilityTypeASR, Protocol: "grpc", Languages: []string{"en-US"}, Models: []string{"mock"}, Streaming: true, MaxConcurrency: 1000}
}

// 作用: 处理 tts Capability 的核心流程。
func ttsCapability(id string) model.Capability {
	return model.Capability{ID: id, Type: model.CapabilityTypeTTS, Protocol: "grpc", Languages: []string{"en-US"}, Streaming: true, MaxConcurrency: 1000}
}

// 作用: 读取测试所需的 must Config Event，失败时终止测试。
func mustConfigEvent(t *testing.T, ch <-chan configcenter.ConfigEvent) configcenter.ConfigEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config event")
	}
	return configcenter.ConfigEvent{}
}

// 作用: 处理 bench Extension 的核心流程。
func benchExtension(i int) string {
	value := "000000" + strings.TrimPrefix(time.Unix(int64(i), 0).UTC().Format("150405"), "00")
	if len(value) > 6 {
		return value[len(value)-6:]
	}
	return value
}

