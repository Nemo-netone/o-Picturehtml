//  通话会话管理：会话创建+状态机转换+etcd持久化
package session_test

import (
	"context"
	"errors"
	"testing"

	pbxerrors "github.com/SATA260/SimulSpeak1/internal/errors"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/session"
)

// 作用: 验证 Test Session_ Create_ C A S Success 场景的行为。
func TestSession_Create_CASSuccess(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	call, err := manager.CreateSession(context.Background(), "call-1", testCreateReq())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if call.ID != "call-1" || call.Owner.Epoch != 1 {
		t.Fatalf("unexpected call session: %#v", call)
	}
}

// 作用: 验证 Test Session_ Create_ Duplicate Call I D 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_Create_DuplicateCallID(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	if _, err := manager.CreateSession(context.Background(), "call-1", testCreateReq()); !errors.Is(err, session.ErrDuplicateCallID) {
		t.Fatalf("expected duplicate call id, got %v", err)
	}
}

// 作用: 验证 Test Session_ Update_ Valid Epoch 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_Update_ValidEpoch(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	if err := manager.UpdateSession(context.Background(), "call-1", 1, session.Update{State: ptrCallState(model.CallStateConnected)}); err != nil {
		t.Fatalf("update session: %v", err)
	}
	got, _ := manager.GetSession(context.Background(), "call-1")
	if got.State != model.CallStateConnected {
		t.Fatalf("expected connected, got %s", got.State)
	}
}

// 作用: 验证 Test Session_ Update_ Epoch Mismatch 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_Update_EpochMismatch(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	err := manager.UpdateSession(context.Background(), "call-1", 99, session.Update{State: ptrCallState(model.CallStateConnected)})
	if !errors.Is(err, pbxerrors.ErrEpochMismatch) {
		t.Fatalf("expected epoch mismatch, got %v", err)
	}
}

// 作用: 验证 Test Session_ Transfer Owner_ Increments Epoch 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_TransferOwner_IncrementsEpoch(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	owner, err := manager.TransferOwner(context.Background(), "call-1", "media-02")
	if err != nil {
		t.Fatalf("transfer owner: %v", err)
	}
	if owner.OwnerNode != "media-02" || owner.Epoch != 2 {
		t.Fatalf("unexpected owner: %#v", owner)
	}
	if err := manager.UpdateSession(context.Background(), "call-1", 1, session.Update{State: ptrCallState(model.CallStateConnected)}); !errors.Is(err, pbxerrors.ErrEpochMismatch) {
		t.Fatalf("stale owner should be fenced, got %v", err)
	}
}

// 作用: 验证 Test Session_ End_ Releases Resources 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_End_ReleasesResources(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	if err := manager.EndSession(context.Background(), "call-1", 1); err != nil {
		t.Fatalf("end session: %v", err)
	}
	got, _ := manager.GetSession(context.Background(), "call-1")
	if got.State != model.CallStateEnded {
		t.Fatalf("expected ended, got %s", got.State)
	}
}

// 作用: 验证 Test Session_ State Transition_ Invalid 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_StateTransition_Invalid(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())

	if err := manager.UpdateSession(context.Background(), "call-1", 1, session.Update{State: ptrCallState(model.CallStateConnected)}); err != nil {
		t.Fatalf("update connected: %v", err)
	}
	if err := manager.UpdateSession(context.Background(), "call-1", 1, session.Update{State: ptrCallState(model.CallStateRinging)}); err == nil {
		t.Fatalf("expected invalid transition")
	}
}

// 作用: 验证 Test Session_ List_ By Tenant Status 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSession_List_ByTenantStatus(t *testing.T) {
	manager := session.New(etcdutil.NewMemoryClient())
	_, _ = manager.CreateSession(context.Background(), "call-1", testCreateReq())
	req := testCreateReq()
	req.TenantID = "tenant-b"
	_, _ = manager.CreateSession(context.Background(), "call-2", req)

	sessions, err := manager.ListSessions(context.Background(), session.Filter{TenantID: "tenant-a", State: model.CallStateRinging})
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "call-1" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
}

// 作用: 处理 test Create Req 的核心流程。
func testCreateReq() session.CreateRequest {
	return session.CreateRequest{
		TenantID:    "tenant-a",
		Caller:      "1001",
		Callee:      "1002",
		OwnerNode:   "media-01",
		GatewayNode: "gw-01",
		MediaNode:   "media-01",
		Participants: []model.Participant{
			{ID: "p-1", Extension: "1001", Role: model.ParticipantRoleCaller},
			{ID: "p-2", Extension: "1002", Role: model.ParticipantRoleCallee},
		},
	}
}

// 作用: 处理 ptr Call State 的核心流程。
func ptrCallState(state model.CallState) *model.CallState {
	return &state
}

