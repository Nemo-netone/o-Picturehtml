//  高可用(HA)：主备选举与故障转移
package ha_test

import (
	"context"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/ha"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
)

// 作用: 验证 Test Fault Detector_ Node Down_ Remove Route 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestFaultDetector_NodeDown_RemoveRoute(t *testing.T) {
	ctx := context.Background()
	reg, sessions := newDeps()
	_ = reg.Register(ctx, mediaNode("media-01", model.NodeStatusUp))
	detector := ha.NewFaultDetector(reg, sessions)

	if err := detector.OnNodeDown(ctx, model.NodeTypeMedia, "media-01"); err != nil {
		t.Fatalf("node down: %v", err)
	}
	if _, err := reg.GetNode(ctx, model.NodeTypeMedia, "media-01"); err == nil {
		t.Fatalf("expected node to be deregistered after node down")
	}
}

// 作用: 验证 Test Fault Detector_ Node Recover_ Rejoin Route 场景的行为。
func TestFaultDetector_NodeRecover_RejoinRoute(t *testing.T) {
	ctx := context.Background()
	reg, sessions := newDeps()
	detector := ha.NewFaultDetector(reg, sessions)
	_ = detector.OnNodeRecover(ctx, mediaNode("media-01", model.NodeStatusUp))

	node, err := reg.GetNode(ctx, model.NodeTypeMedia, "media-01")
	if err != nil || node.Status != model.NodeStatusUp {
		t.Fatalf("expected recovered node to be registered: node=%#v err=%v", node, err)
	}
}

// 作用: 验证 Test Fault Detector_ Mark Sessions Suspect 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestFaultDetector_MarkSessionsSuspect(t *testing.T) {
	ctx := context.Background()
	reg, sessions := newDeps()
	_ = reg.Register(ctx, mediaNode("media-01", model.NodeStatusUp))
	call, _ := sessions.CreateSession(ctx, "call-1", session.CreateRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002", OwnerNode: "media-01", MediaNode: "media-01", GatewayNode: "gw-01"})
	_ = sessions.UpdateSession(ctx, call.ID, call.Owner.Epoch, session.Update{State: ptrCallState(model.CallStateConnected)})
	detector := ha.NewFaultDetector(reg, sessions)

	if err := detector.OnNodeDown(ctx, model.NodeTypeMedia, "media-01"); err != nil {
		t.Fatalf("node down: %v", err)
	}
	got, _ := sessions.GetSession(ctx, "call-1")
	if got.State != model.CallStateSuspect {
		t.Fatalf("expected suspect, got %s", got.State)
	}
}

// 作用: 验证 Test Election_ Single Leader 场景的行为。
func TestElection_SingleLeader(t *testing.T) {
	election := ha.NewElection()
	if ok := election.TryAcquire("cleanup", "control-1"); !ok {
		t.Fatalf("first candidate should acquire")
	}
	if ok := election.TryAcquire("cleanup", "control-2"); ok {
		t.Fatalf("second candidate should not acquire")
	}
}

// 作用: 验证 Test Draining_ No New Calls 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestDraining_NoNewCalls(t *testing.T) {
	ctx := context.Background()
	reg, _ := newDeps()
	_ = reg.Register(ctx, mediaNode("media-01", model.NodeStatusUp))
	drain := ha.NewDrainController(reg)
	if err := drain.Start(ctx, model.NodeTypeMedia, "media-01"); err != nil {
		t.Fatalf("drain: %v", err)
	}
	node, err := reg.GetNode(ctx, model.NodeTypeMedia, "media-01")
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if node.Status != model.NodeStatusDraining {
		t.Fatalf("expected draining node, got %s", node.Status)
	}
}

// 作用: 处理 new Deps 的核心流程。
func newDeps() (*registry.Registry, *session.Manager) {
	client := etcdutil.NewMemoryClient()
	return registry.New(client, registry.Options{}), session.New(client)
}

// 作用: 处理 media Node 的核心流程。
func mediaNode(id string, status model.NodeStatus) *model.Node {
	return &model.Node{ID: id, Type: model.NodeTypeMedia, Endpoint: "127.0.0.1:8021", Status: status, MaxCalls: 100, Weight: 100}
}

// 作用: 处理 ptr Call State 的核心流程。
func ptrCallState(state model.CallState) *model.CallState {
	return &state
}

