//  端到端测试：完整同传流程验证
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
	"github.com/pion/rtp"
	pionwebrtc "github.com/pion/webrtc/v4"
)

// TestRunningBackendWebRTCProbe 连接正在运行的 PBX 后端，验证 WebSocket 信令、WebRTC ICE 和音频 RTP 上行链路。
func TestRunningBackendWebRTCProbe(t *testing.T) {
	baseURL := os.Getenv("GO_PBX_BACKEND_URL")
	if baseURL == "" {
		t.Skip("set GO_PBX_BACKEND_URL=http://127.0.0.1:8080 to verify a running backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := sdk.New(baseURL)
	if err != nil {
		t.Fatalf("new sdk client: %v", err)
	}
	control, ack, err := client.ConnectWebSocket(ctx, sdk.WSConnectOptions{
		TenantID: "tenant-a",
		ClientID: "go-webrtc-probe",
	})
	if err != nil {
		t.Fatalf("connect backend websocket: %v", err)
	}
	defer func() {
		_ = control.Close()
	}()
	t.Logf("后端 WebSocket 已连接: connectionID=%s ack=%s", control.ConnectionID(), ack.Type)

	peer, track := newProbeAudioPeer(t)
	defer func() {
		if err := peer.Close(); err != nil {
			t.Logf("close probe peer: %v", err)
		}
	}()

	stateChanges := watchProbePeerState(peer)
	messages, readErrors := readBackendMessages(control)
	offer := createProbeOffer(t, peer)
	if err := control.SendWebRTCOffer(ctx, "probe-call-001", "probe-user-001", offer.SDP); err != nil {
		t.Fatalf("send webrtc offer: %v", err)
	}

	waitBackendAnswerAndICE(ctx, t, peer, messages, readErrors)
	waitProbeConnected(ctx, t, peer, stateChanges)
	writeProbeAudioRTP(ctx, t, track)
	t.Log("后端验证完成: WebSocket 信令已通过，WebRTC 已 connected，音频 RTP 已发送")
}

// newProbeAudioPeer 创建 Go 探测客户端 PeerConnection 和 Opus RTP 发送轨道。
func newProbeAudioPeer(t *testing.T) (*pionwebrtc.PeerConnection, *pionwebrtc.TrackLocalStaticRTP) {
	t.Helper()
	peer, err := pionwebrtc.NewPeerConnection(pionwebrtc.Configuration{})
	if err != nil {
		t.Fatalf("new probe peer: %v", err)
	}
	track, err := pionwebrtc.NewTrackLocalStaticRTP(
		pionwebrtc.RTPCodecCapability{MimeType: pionwebrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio",
		"go-backend-probe",
	)
	if err != nil {
		t.Fatalf("new probe audio track: %v", err)
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		t.Fatalf("add probe audio track: %v", err)
	}
	go discardProbeRTCP(sender)
	return peer, track
}

// discardProbeRTCP 持续读取发送端 RTCP，避免探测端反馈包堆积阻塞。
func discardProbeRTCP(sender *pionwebrtc.RTPSender) {
	buffer := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}

// watchProbePeerState 监听探测端 PeerConnection 状态变化，供测试等待 connected 或 failed。
func watchProbePeerState(peer *pionwebrtc.PeerConnection) <-chan pionwebrtc.PeerConnectionState {
	states := make(chan pionwebrtc.PeerConnectionState, 16)
	peer.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		select {
		case states <- state:
		default:
		}
	})
	return states
}

// readBackendMessages 后台读取 PBX WebSocket 消息，避免信令处理和 ICE 连接等待互相阻塞。
func readBackendMessages(control *sdk.WebSocket) (<-chan sdk.WSMessage, <-chan error) {
	messages := make(chan sdk.WSMessage, 32)
	errors := make(chan error, 1)
	go func() {
		defer close(messages)
		for {
			message, err := control.Read(context.Background())
			if err != nil {
				errors <- err
				return
			}
			messages <- message
		}
	}()
	return messages, errors
}

// createProbeOffer 创建并等待 ICE 收集完成，把探测端 host candidates 内嵌在 offer SDP 中。
func createProbeOffer(t *testing.T, peer *pionwebrtc.PeerConnection) pionwebrtc.SessionDescription {
	t.Helper()
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create probe offer: %v", err)
	}
	gatherComplete := pionwebrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set probe local offer: %v", err)
	}
	<-gatherComplete
	if peer.LocalDescription() == nil {
		t.Fatalf("probe local description is nil")
	}
	return *peer.LocalDescription()
}

// waitBackendAnswerAndICE 等待后端 answer，并应用后端通过 WebSocket 返回的 ICE candidate。
func waitBackendAnswerAndICE(ctx context.Context, t *testing.T, peer *pionwebrtc.PeerConnection, messages <-chan sdk.WSMessage, readErrors <-chan error) {
	t.Helper()
	var pendingICE []pionwebrtc.ICECandidateInit
	answerApplied := false
	for !answerApplied {
		select {
		case <-ctx.Done():
			t.Fatalf("wait backend answer: %v", ctx.Err())
		case err := <-readErrors:
			t.Fatalf("read backend websocket: %v", err)
		case message := <-messages:
			if message.Type == "error" {
				t.Fatalf("backend websocket error: %s", message.Error)
			}
			switch message.Type {
			case "webrtc_answer":
				if message.SDP == "" {
					t.Fatalf("backend answer has empty SDP: %#v", message)
				}
				if err := peer.SetRemoteDescription(pionwebrtc.SessionDescription{Type: pionwebrtc.SDPTypeAnswer, SDP: message.SDP}); err != nil {
					t.Fatalf("apply backend answer: %v", err)
				}
				answerApplied = true
				applyProbeICECandidates(t, peer, pendingICE)
			case "ice":
				candidate, err := parseProbeICECandidate(message.Candidate)
				if err != nil {
					t.Fatalf("parse backend ice: %v", err)
				}
				pendingICE = append(pendingICE, candidate)
			}
		}
	}
	go applyBackendICEUntilDone(ctx, t, peer, messages, readErrors)
}

// applyBackendICEUntilDone 在 answer 应用后持续处理后端 trickle ICE candidate。
func applyBackendICEUntilDone(ctx context.Context, t *testing.T, peer *pionwebrtc.PeerConnection, messages <-chan sdk.WSMessage, readErrors <-chan error) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case <-readErrors:
			return
		case message, ok := <-messages:
			if !ok {
				return
			}
			if message.Type != "ice" {
				continue
			}
			candidate, err := parseProbeICECandidate(message.Candidate)
			if err != nil {
				t.Errorf("parse backend ice: %v", err)
				continue
			}
			applyProbeICECandidates(t, peer, []pionwebrtc.ICECandidateInit{candidate})
		}
	}
}

// parseProbeICECandidate 解析后端返回的 JSON ICECandidateInit 或纯 candidate 字符串。
func parseProbeICECandidate(raw string) (pionwebrtc.ICECandidateInit, error) {
	if raw == "" {
		return pionwebrtc.ICECandidateInit{}, fmt.Errorf("empty ICE candidate")
	}
	if raw[0] != '{' {
		return pionwebrtc.ICECandidateInit{Candidate: raw}, nil
	}
	var candidate pionwebrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
		return pionwebrtc.ICECandidateInit{}, err
	}
	return candidate, nil
}

// applyProbeICECandidates 批量应用后端 ICE candidate。
func applyProbeICECandidates(t *testing.T, peer *pionwebrtc.PeerConnection, candidates []pionwebrtc.ICECandidateInit) {
	t.Helper()
	for _, candidate := range candidates {
		if err := peer.AddICECandidate(candidate); err != nil {
			t.Fatalf("apply backend ice candidate: %v", err)
		}
	}
}

// waitProbeConnected 等待探测端 PeerConnection 建立成功。
func waitProbeConnected(ctx context.Context, t *testing.T, peer *pionwebrtc.PeerConnection, states <-chan pionwebrtc.PeerConnectionState) {
	t.Helper()
	for {
		switch current := peer.ConnectionState(); current {
		case pionwebrtc.PeerConnectionStateConnected:
			return
		case pionwebrtc.PeerConnectionStateFailed, pionwebrtc.PeerConnectionStateClosed:
			t.Fatalf("probe peer state is %s", current.String())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait probe connected: %v, last state=%s", ctx.Err(), peer.ConnectionState().String())
		case state := <-states:
			t.Logf("探测端 WebRTC 状态变化: %s", state.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// writeProbeAudioRTP 发送几帧 Opus RTP，用于触发后端控制台的音频包日志。
func writeProbeAudioRTP(ctx context.Context, t *testing.T, track *pionwebrtc.TrackLocalStaticRTP) {
	t.Helper()
	for sequence := uint16(1); sequence <= 5; sequence++ {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: sequence,
				Timestamp:      uint32(sequence) * 960,
				SSRC:           4321,
			},
			Payload: []byte{0xf8, 0xff, 0xfe},
		}
		if err := track.WriteRTP(packet); err != nil {
			t.Fatalf("write probe RTP: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("write probe RTP: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

