//  WebRTC管理器连通性测试
package webrtc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pion/rtp"
	pionwebrtc "github.com/pion/webrtc/v4"
)

// TestManagerEstablishesWebRTCWithGoPeer 验证 Go 模拟客户端能与 PBX WebRTC Manager 完成 ICE 连接并发送音频 RTP。
func TestManagerEstablishesWebRTCWithGoPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

	const connectionID = "go-loopback-conn"
	manager := NewManager()
	defer manager.CloseConnection(connectionID)

	clientPeer, audioTrack := newGoAudioPeer(t)
	defer func() {
		if err := clientPeer.Close(); err != nil {
			t.Logf("close client peer: %v", err)
		}
	}()
	defer cancel()

	serverCandidates := make(chan string, 32)
	offer := createGatheredOffer(t, clientPeer)
	answer, err := manager.AcceptOffer(ctx, OfferRequest{
		ConnectionID: connectionID,
		CallID:       "go-loopback-call",
		UserID:       "go-loopback-user",
		SDP:          offer.SDP,
		OnICE: func(candidate string) {
			select {
			case serverCandidates <- candidate:
			default:
				t.Logf("server candidate queue full, drop candidate: %s", truncateLogValue(candidate, 120))
			}
		},
	})
	if err != nil {
		t.Fatalf("accept offer: %v", err)
	}
	if err := clientPeer.SetRemoteDescription(pionwebrtc.SessionDescription{Type: pionwebrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatalf("set client remote answer: %v", err)
	}
	applyServerCandidates(ctx, t, clientPeer, serverCandidates)

	session := manager.session(connectionID)
	if session == nil {
		t.Fatalf("expected manager session")
	}
	waitForPeerConnected(ctx, t, "client", clientPeer.ConnectionState)
	waitForPeerConnected(ctx, t, "server", session.peer.ConnectionState)
	writeAudioRTP(ctx, t, audioTrack)
	waitForAudioPacket(ctx, t, session)
}

// newGoAudioPeer 创建带 Opus 音频发送轨道的 Go 侧 PeerConnection，用于模拟浏览器麦克风上行。
func newGoAudioPeer(t *testing.T) (*pionwebrtc.PeerConnection, *pionwebrtc.TrackLocalStaticRTP) {
	t.Helper()
	peer, err := pionwebrtc.NewPeerConnection(pionwebrtc.Configuration{})
	if err != nil {
		t.Fatalf("new client peer: %v", err)
	}
	track, err := pionwebrtc.NewTrackLocalStaticRTP(
		pionwebrtc.RTPCodecCapability{MimeType: pionwebrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio",
		"go-test",
	)
	if err != nil {
		t.Fatalf("new audio track: %v", err)
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		t.Fatalf("add audio track: %v", err)
	}
	go discardRTCP(sender)
	return peer, track
}

// discardRTCP 持续读取 RTCP，避免发送端因为反馈包无人读取而阻塞。
func discardRTCP(sender *pionwebrtc.RTPSender) {
	buffer := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}

// createGatheredOffer 创建 offer 并等待本地 ICE 收集完成，让客户端候选直接内嵌进 SDP。
func createGatheredOffer(t *testing.T, peer *pionwebrtc.PeerConnection) pionwebrtc.SessionDescription {
	t.Helper()
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := pionwebrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set client local offer: %v", err)
	}
	<-gatherComplete
	if peer.LocalDescription() == nil {
		t.Fatalf("client local description is nil")
	}
	return *peer.LocalDescription()
}

// applyServerCandidates 异步应用 PBX 侧 trickle ICE candidate。
func applyServerCandidates(ctx context.Context, t *testing.T, peer *pionwebrtc.PeerConnection, candidates <-chan string) {
	t.Helper()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-candidates:
				candidate, err := parseICECandidate(raw)
				if err != nil {
					t.Errorf("parse server candidate: %v", err)
					continue
				}
				if err := peer.AddICECandidate(candidate); err != nil {
					t.Errorf("add server candidate: %v", err)
				}
			}
		}
	}()
}

// waitForPeerConnected 等待 PeerConnection 进入 connected/completed 状态，失败或超时则输出当前状态。
func waitForPeerConnected(ctx context.Context, t *testing.T, name string, state func() pionwebrtc.PeerConnectionState) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch current := state(); current {
		case pionwebrtc.PeerConnectionStateConnected:
			return
		case pionwebrtc.PeerConnectionStateFailed, pionwebrtc.PeerConnectionStateClosed:
			t.Fatalf("%s peer state is %s", name, current.String())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait %s peer connected: %v, last state=%s", name, ctx.Err(), state().String())
		case <-ticker.C:
		}
	}
}

// writeAudioRTP 写入几帧 Opus RTP 包，用于触发 PBX 后端 OnTrack 和 ReadRTP。
func writeAudioRTP(ctx context.Context, t *testing.T, track *pionwebrtc.TrackLocalStaticRTP) {
	t.Helper()
	for sequence := uint16(1); sequence <= 5; sequence++ {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: sequence,
				Timestamp:      uint32(sequence) * 960,
				SSRC:           1234,
			},
			Payload: []byte{0xf8, 0xff, 0xfe},
		}
		if err := track.WriteRTP(packet); err != nil {
			t.Fatalf("write audio RTP: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("write audio RTP: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitForAudioPacket 等待 PBX 后端读取到至少一个音频 RTP 包。
func waitForAudioPacket(ctx context.Context, t *testing.T, session *Session) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if session.audioPacketSeen.Load() {
			return
		}
		select {
		case <-ctx.Done():
			dump, _ := json.Marshal(session.candidateSnapshot())
			t.Fatalf("wait audio packet: %v, candidates=%s", ctx.Err(), dump)
		case <-ticker.C:
		}
	}
}

