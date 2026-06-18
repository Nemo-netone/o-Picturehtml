//  网关：信令网关抽象
package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/gateway"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test Gateway_ W S Connect_ Valid Token 场景的行为。
func TestGateway_WSConnect_ValidToken(t *testing.T) {
	gw := newGateway()
	conn, err := gw.Connect(context.Background(), "tenant-a:1001")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if conn.TenantID != "tenant-a" || conn.Extension != "1001" {
		t.Fatalf("unexpected connection: %#v", conn)
	}
}

// 作用: 验证 Test Gateway_ W S Connect_ Invalid Token 场景的行为。
func TestGateway_WSConnect_InvalidToken(t *testing.T) {
	gw := newGateway()
	if _, err := gw.Connect(context.Background(), "bad-token"); !errors.Is(err, gateway.ErrInvalidToken) {
		t.Fatalf("expected invalid token, got %v", err)
	}
}

// 作用: 验证 Test Gateway_ Presence_ Online Offline 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestGateway_Presence_OnlineOffline(t *testing.T) {
	gw := newGateway()
	conn, _ := gw.Connect(context.Background(), "tenant-a:1001")
	presence, err := gw.GetPresence(context.Background(), "tenant-a", "1001")
	if err != nil {
		t.Fatalf("get presence: %v", err)
	}
	if presence.Status != model.PresenceStatusOnline {
		t.Fatalf("expected online, got %s", presence.Status)
	}

	if err := gw.Disconnect(context.Background(), conn.ID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	presence, _ = gw.GetPresence(context.Background(), "tenant-a", "1001")
	if presence.Status != model.PresenceStatusOffline {
		t.Fatalf("expected offline, got %s", presence.Status)
	}
}

// 作用: 验证 Test Gateway_ S D P_ Offer Answer Forward 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestGateway_SDP_OfferAnswerForward(t *testing.T) {
	gw := newGateway()
	conn, _ := gw.Connect(context.Background(), "tenant-a:1001")

	_ = gw.SendOffer(context.Background(), conn.ID, "offer-sdp")
	_ = gw.SendAnswer(context.Background(), conn.ID, "answer-sdp")
	msgs := gw.Messages(conn.ID)
	if len(msgs) != 2 || msgs[0].Type != gateway.MessageOffer || msgs[1].Type != gateway.MessageAnswer {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
}

// 作用: 验证 Test Gateway_ I C E Candidate Forward 场景的行为。
func TestGateway_ICECandidateForward(t *testing.T) {
	gw := newGateway()
	conn, _ := gw.Connect(context.Background(), "tenant-a:1001")

	_ = gw.SendICE(context.Background(), conn.ID, "candidate")
	msgs := gw.Messages(conn.ID)
	if len(msgs) != 1 || msgs[0].Type != gateway.MessageICE || msgs[0].Payload != "candidate" {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
}

// 作用: 验证 Test Gateway_ Heartbeat Timeout 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestGateway_HeartbeatTimeout(t *testing.T) {
	gw := gateway.New(gateway.Options{HeartbeatTimeout: 10 * time.Millisecond, MaxConnections: 10})
	conn, _ := gw.Connect(context.Background(), "tenant-a:1001")
	time.Sleep(20 * time.Millisecond)

	if err := gw.CloseIdle(context.Background()); err != nil {
		t.Fatalf("close idle: %v", err)
	}
	presence, _ := gw.GetPresence(context.Background(), "tenant-a", "1001")
	if presence.Status != model.PresenceStatusOffline {
		t.Fatalf("expected offline after heartbeat timeout")
	}
	if _, err := gw.GetConnection(conn.ID); !errors.Is(err, gateway.ErrConnectionNotFound) {
		t.Fatalf("expected connection removed, got %v", err)
	}
}

// 作用: 验证 Test Gateway_ Reconnect_ Recover Session 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestGateway_Reconnect_RecoverSession(t *testing.T) {
	gw := newGateway()
	conn, _ := gw.Connect(context.Background(), "tenant-a:1001")
	_ = gw.BindCall(conn.ID, "call-1")
	_ = gw.Disconnect(context.Background(), conn.ID)

	recovered, err := gw.RecoverSession(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.CallID != "call-1" {
		t.Fatalf("expected call-1, got %s", recovered.CallID)
	}
}

// 作用: 验证 Test Gateway_ Concurrency Limit 场景的行为。
func TestGateway_ConcurrencyLimit(t *testing.T) {
	gw := gateway.New(gateway.Options{MaxConnections: 1, HeartbeatTimeout: time.Minute})
	_, _ = gw.Connect(context.Background(), "tenant-a:1001")
	if _, err := gw.Connect(context.Background(), "tenant-a:1002"); !errors.Is(err, gateway.ErrTooManyConnections) {
		t.Fatalf("expected too many connections, got %v", err)
	}
}

// 作用: 验证 Test Gateway_ Provider Configs_ Snapshot 场景的行为。
// 逻辑: 保存 provider 配置后修改原始 map，断言网关内部保存的是独立快照。
func TestGateway_ProviderConfigsSnapshot(t *testing.T) {
	gw := newGateway()
	conn, _ := gw.Connect(context.Background(), "tenant-a:relay-service")
	configs := map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {
			Provider: "tencent-asr",
			Secrets:  map[string]string{"secretKey": "secret-key"},
		},
	}
	if err := gw.SetProviderConfigs(conn.ID, configs); err != nil {
		t.Fatalf("set provider configs: %v", err)
	}
	configs[model.CapabilityTypeASR] = model.ProviderConfig{Provider: "changed"}

	got := gw.ProviderConfigs(conn.ID)
	if got[model.CapabilityTypeASR].Provider != "tencent-asr" || got[model.CapabilityTypeASR].Secrets["secretKey"] != "secret-key" {
		t.Fatalf("expected provider config snapshot, got %#v", got)
	}
}

// 作用: 处理 new Gateway 的核心流程。
func newGateway() *gateway.Gateway {
	return gateway.New(gateway.Options{MaxConnections: 10, HeartbeatTimeout: time.Minute})
}

