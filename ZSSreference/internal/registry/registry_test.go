//  服务注册中心：etcd注册+节点发现+心跳+Watch
package registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// 作用: 验证 Test Registry_ Register_ With Lease 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_Register_WithLease(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{LeaseTTL: time.Second})

	node := testMediaNode("media-01")
	if err := store.Register(ctx, node); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := store.GetNode(ctx, model.NodeTypeMedia, "media-01")
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.ID != node.ID || got.Type != node.Type {
		t.Fatalf("unexpected node: %#v", got)
	}
	if got.LeaseID == 0 {
		t.Fatalf("expected lease id to be written")
	}
}

// 作用: 验证 Test Registry_ Deregister_ Removes Key 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_Deregister_RemovesKey(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{LeaseTTL: time.Second})

	if err := store.Register(ctx, testMediaNode("media-01")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := store.Deregister(ctx, model.NodeTypeMedia, "media-01"); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	if _, err := store.GetNode(ctx, model.NodeTypeMedia, "media-01"); err == nil {
		t.Fatalf("expected get after deregister to fail")
	}
}

// 作用: 验证 Test Registry_ Lease Expired_ Key Deleted 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_LeaseExpired_KeyDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := etcdutil.NewMemoryClient()
	store := registry.New(client, registry.Options{LeaseTTL: 30 * time.Millisecond})

	if err := store.Register(ctx, testMediaNode("media-01")); err != nil {
		t.Fatalf("register: %v", err)
	}
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for lease expiration")
		default:
			if _, err := store.GetNode(context.Background(), model.NodeTypeMedia, "media-01"); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// 作用: 验证 Test Registry_ List Nodes_ By Type 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_ListNodes_ByType(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{})

	_ = store.Register(ctx, testMediaNode("media-01"))
	_ = store.Register(ctx, testMediaNode("media-02"))
	_ = store.Register(ctx, &model.Node{ID: "gw-01", Type: model.NodeTypeSignaling, Endpoint: "127.0.0.1:8081", Status: model.NodeStatusUp})

	nodes, err := store.ListNodes(ctx, model.NodeTypeMedia)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected two media nodes, got %d", len(nodes))
	}
}

// 作用: 验证 Test Registry_ Watch Node_ Online Offline 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_WatchNode_OnlineOffline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{})

	events := store.WatchNodes(ctx, model.NodeTypeMedia)
	if err := store.Register(ctx, testMediaNode("media-01")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := store.Deregister(ctx, model.NodeTypeMedia, "media-01"); err != nil {
		t.Fatalf("deregister: %v", err)
	}

	first := mustReadNodeEvent(t, events)
	if first.Type != registry.NodeEventUp {
		t.Fatalf("expected up event, got %s", first.Type)
	}
	second := mustReadNodeEvent(t, events)
	if second.Type != registry.NodeEventDown {
		t.Fatalf("expected down event, got %s", second.Type)
	}
}

// 作用: 验证 Test Registry_ Register Capability 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_RegisterCapability(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	capability := testASRCapability("asr-01")

	if err := store.RegisterCapability(ctx, capability); err != nil {
		t.Fatalf("register capability: %v", err)
	}

	got, err := store.ListCapabilities(ctx, model.CapabilitySelector{Type: model.CapabilityTypeASR})
	if err != nil {
		t.Fatalf("list capabilities: %v", err)
	}
	if len(got) != 1 || got[0].ID != capability.ID {
		t.Fatalf("unexpected capabilities: %#v", got)
	}
}

// 作用: 验证 Test Registry_ Capability Selector_ Language Model 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_CapabilitySelector_LanguageModel(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	_ = store.RegisterCapability(ctx, testASRCapability("asr-zh"))
	other := testASRCapability("asr-en")
	other.Languages = []string{"en-US"}
	_ = store.RegisterCapability(ctx, other)

	got, err := store.ListCapabilities(ctx, model.CapabilitySelector{
		Type:     model.CapabilityTypeASR,
		Language: "zh-CN",
		Model:    "paraformer-streaming",
	})
	if err != nil {
		t.Fatalf("list capabilities: %v", err)
	}
	if len(got) != 1 || got[0].ID != "asr-zh" {
		t.Fatalf("unexpected selected capabilities: %#v", got)
	}
}

// 作用: 验证 Test Registry_ Update Load 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRegistry_UpdateLoad(t *testing.T) {
	ctx := context.Background()
	store := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	_ = store.Register(ctx, testMediaNode("media-01"))

	if err := store.UpdateLoad(ctx, model.NodeTypeMedia, "media-01", 42); err != nil {
		t.Fatalf("update load: %v", err)
	}
	got, err := store.GetNode(ctx, model.NodeTypeMedia, "media-01")
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.CurrentCalls != 42 {
		t.Fatalf("expected current calls 42, got %d", got.CurrentCalls)
	}
}

// 作用: 处理 test Media Node 的核心流程。
func testMediaNode(id string) *model.Node {
	return &model.Node{
		ID:           id,
		Type:         model.NodeTypeMedia,
		Endpoint:     "127.0.0.1:8021",
		Zone:         "az-a",
		Status:       model.NodeStatusUp,
		Weight:       100,
		MaxCalls:     1000,
		CurrentCalls: 0,
	}
}

// 作用: 处理 test A S R Capability 的核心流程。
func testASRCapability(id string) model.Capability {
	return model.Capability{
		ID:                 id,
		Type:               model.CapabilityTypeASR,
		Protocol:           "grpc",
		Endpoint:           "127.0.0.1:9001",
		Languages:          []string{"zh-CN", "en-US"},
		Models:             []string{"paraformer-streaming"},
		Streaming:          true,
		MaxConcurrency:     32,
		CurrentConcurrency: 4,
	}
}

// 作用: 读取测试所需的 must Read Node Event，失败时终止测试。
func mustReadNodeEvent(t *testing.T, ch <-chan registry.NodeEvent) registry.NodeEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for node event")
	}
	return registry.NodeEvent{}
}

