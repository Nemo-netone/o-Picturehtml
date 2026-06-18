//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/api-server/httpapi"
	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/gateway"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxprotocol "github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
	pionwebrtc "github.com/pion/webrtc/v4"
)

// TestClient_Health 验证 SDK 健康检查。
func TestClient_Health(t *testing.T) {
	ctx := context.Background()
	client := newSDKClient(t)

	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.Status != "ok" || health.Service == "" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

// TestClient_RouteCallWithSeededTopology 验证 PBX 自动注册后的拓扑可以完成 AI 通话路由。
func TestClient_RouteCallWithSeededTopology(t *testing.T) {
	ctx := context.Background()
	harness := newSDKHarness(t)

	if err := harness.Registry.Register(ctx, &model.Node{
		ID:       "media-sdk",
		Type:     model.NodeTypeMedia,
		Endpoint: "127.0.0.1:8021",
		Zone:     "az-a",
		Status:   model.NodeStatusUp,
		MaxCalls: 100,
	}); err != nil {
		t.Fatalf("seed media node: %v", err)
	}
	for _, capability := range []model.Capability{
		{ID: "vad-sdk", Type: model.CapabilityTypeVAD, Protocol: "grpc", MaxConcurrency: 100},
		{ID: "asr-sdk", Type: model.CapabilityTypeASR, Protocol: "grpc", Languages: []string{"zh-CN"}, Models: []string{"general"}, MaxConcurrency: 100},
		{ID: "tts-sdk", Type: model.CapabilityTypeTTS, Protocol: "grpc", Languages: []string{"zh-CN"}, Models: []string{"voice-a"}, MaxConcurrency: 100},
	} {
		if err := harness.Registry.RegisterCapability(ctx, capability); err != nil {
			t.Fatalf("seed capability %s: %v", capability.ID, err)
		}
	}

	route, err := harness.Client.RouteCall(ctx, sdk.RouteRequest{
		TenantID: "tenant-a",
		Caller:   "1001",
		Callee:   "1002",
		NeedAI:   true,
		Language: "zh-CN",
		ASRModel: "general",
	})
	if err != nil {
		t.Fatalf("route call: %v", err)
	}
	if route.CallID == "" || route.MediaNode == "" || route.AIPipeline == nil {
		t.Fatalf("unexpected route result: %#v", route)
	}

	call, err := harness.Client.GetCall(ctx, route.CallID)
	if err != nil {
		t.Fatalf("get call: %v", err)
	}
	if call.ID != route.CallID {
		t.Fatalf("unexpected call: %#v", call)
	}

	ended, err := harness.Client.EndCall(ctx, route.CallID)
	if err != nil {
		t.Fatalf("end call: %v", err)
	}
	if ended.CallID != route.CallID {
		t.Fatalf("unexpected end result: %#v", ended)
	}
}

// TestClient_ExtensionPresence 验证 SDK 可以读取服务端已有的分机和 presence。
func TestClient_ExtensionPresence(t *testing.T) {
	ctx := context.Background()
	harness := newSDKHarness(t)

	ext := model.Extension{
		ID:          "ext-1001",
		TenantID:    "tenant-a",
		Extension:   "1001",
		Status:      model.ExtensionStatusEnabled,
		DisplayName: "Alice",
		Presence: &model.Presence{
			TenantID:     "tenant-a",
			Extension:    "1001",
			GatewayID:    "gateway-sdk",
			Status:       model.PresenceStatusOnline,
			ConnectionID: "conn-1001",
			UpdatedAt:    time.Now().UTC(),
		},
	}
	if err := harness.Config.SetExtension(ctx, ext); err != nil {
		t.Fatalf("seed extension: %v", err)
	}

	got, err := harness.Client.GetExtension(ctx, "tenant-a", "1001")
	if err != nil {
		t.Fatalf("get extension: %v", err)
	}
	if got.DisplayName != "Alice" {
		t.Fatalf("unexpected stored extension: %#v", got)
	}

	presence, err := harness.Client.GetPresence(ctx, "tenant-a", "1001")
	if err != nil {
		t.Fatalf("get presence: %v", err)
	}
	if presence.Status != model.PresenceStatusOnline {
		t.Fatalf("unexpected presence: %#v", presence)
	}
}

// TestClient_APIError 验证 SDK 会把错误响应转换成 APIError。
func TestClient_APIError(t *testing.T) {
	client := newSDKClient(t)

	_, err := client.GetCall(context.Background(), "missing-call")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *sdk.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status: %#v", apiErr)
	}
}

// TestNew_RequiresAbsoluteBaseURL 验证 SDK 初始化时会校验 baseURL。
func TestNew_RequiresAbsoluteBaseURL(t *testing.T) {
	if _, err := sdk.New(""); !errors.Is(err, sdk.ErrMissingBaseURL) {
		t.Fatalf("expected missing base url, got %v", err)
	}
	if _, err := sdk.New("localhost:8080"); err == nil {
		t.Fatal("expected absolute URL error")
	}
}

// TestClient_WebSocketRelayFlow 验证业务服务通过 SDK 建立到 PBX 的 WebSocket 控制连接。
func TestClient_WebSocketRelayFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw := gateway.New(gateway.Options{})
	client := newSDKClientWithGateway(t, gw,
		sdk.WithTencentASR(sdk.TencentASRConfig{
			AppID:           "1250000000",
			SecretID:        "secret-id",
			SecretKey:       "secret-key",
			EngineModelType: "16k_en",
			VoiceFormat:     "opus",
			NeedVAD:         true,
		}),
	)

	conn, ack, err := client.ConnectWebSocket(ctx, sdk.WSConnectOptions{
		TenantID: "tenant-a",
		ClientID: "relay-service",
	})
	if err != nil {
		t.Fatalf("connect websocket: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()
	if ack.Type != "client_hello_ack" || conn.ConnectionID() == "" {
		t.Fatalf("expected connection id, got ack=%#v connectionID=%q", ack, conn.ConnectionID())
	}
	asrConfig := ack.ProviderConfigs[model.CapabilityTypeASR]
	if asrConfig.Provider != sdk.TencentASRProvider || asrConfig.Secrets["secretKey"] != "[REDACTED]" {
		t.Fatalf("expected redacted tencent asr config in ack: %#v", ack.ProviderConfigs)
	}

	stored := gw.ProviderConfigs(conn.ConnectionID())
	if stored[model.CapabilityTypeASR].Secrets["secretKey"] != "secret-key" {
		t.Fatalf("expected gateway to keep real secret internally: %#v", stored)
	}

	if err := conn.SendWebRTCOffer(ctx, "call-1", "user-1", newTestAudioOffer(t)); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	offerAck, err := readWSMessageType(ctx, t, conn, "webrtc_offer_ack")
	if err != nil {
		t.Fatalf("read offer ack: %v", err)
	}
	if offerAck.Type != "webrtc_offer_ack" || offerAck.CallID != "call-1" {
		t.Fatalf("unexpected offer ack: %#v", offerAck)
	}
	answer, err := readWSMessageType(ctx, t, conn, "webrtc_answer")
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	if answer.SDP == "" {
		t.Fatalf("expected answer sdp: %#v", answer)
	}

	if err := conn.SendICE(ctx, "call-1", "user-1", "candidate"); err != nil {
		t.Fatalf("send ice: %v", err)
	}
	iceAck, err := readWSMessageType(ctx, t, conn, "ice_ack")
	if err != nil {
		t.Fatalf("read ice ack: %v", err)
	}
	if iceAck.Type != "ice_ack" {
		t.Fatalf("unexpected ice ack: %#v", iceAck)
	}

	if err := conn.SendTTSCommand(ctx, "call-1", "user-1", "请稍等", "101001", "zh-CN"); err != nil {
		t.Fatalf("send tts command: %v", err)
	}
	ttsAck, err := readWSMessageType(ctx, t, conn, "tts_command_ack")
	if err != nil {
		t.Fatalf("read tts ack: %v", err)
	}
	if ttsAck.Type != "tts_command_ack" || ttsAck.Text != "请稍等" {
		t.Fatalf("unexpected tts ack: %#v", ttsAck)
	}
}

// TestClient_WebSocketCompactRelayFlow 验证 compact 模式只回传中介服务需要转发或消费的业务消息。
func TestClient_WebSocketCompactRelayFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := newSDKClientWithGateway(t, gateway.New(gateway.Options{}))

	conn, ack, err := client.ConnectWebSocket(ctx, sdk.WSConnectOptions{
		TenantID:     "tenant-a",
		ClientID:     "relay-service",
		ResponseMode: sdk.WSResponseModeCompact,
	})
	if err != nil {
		t.Fatalf("connect websocket: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()
	if ack.Type != "client_hello_ack" || ack.ResponseMode != sdk.WSResponseModeCompact {
		t.Fatalf("expected compact hello ack, got %#v", ack)
	}

	if err := conn.SendWebRTCOffer(ctx, "call-compact", "user-1", newTestAudioOffer(t)); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	answer, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	if answer.Type != "webrtc_answer" || answer.SDP == "" {
		t.Fatalf("expected compact flow to return answer directly, got %#v", answer)
	}

	if err := conn.SendICE(ctx, "call-compact", "user-1", "candidate"); err != nil {
		t.Fatalf("send ice: %v", err)
	}
	if err := conn.SendTTSCommand(ctx, "call-compact", "user-1", "请稍等", "101001", "zh-CN"); err != nil {
		t.Fatalf("send tts command: %v", err)
	}
	ttsResult, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read tts result: %v", err)
	}
	if ttsResult.Type != "tts_result" || ttsResult.Audio != "" || ttsResult.Text != "请稍等" {
		t.Fatalf("expected compact tts result without websocket audio, got %#v", ttsResult)
	}
	if ttsResult.Metadata["audioTransport"] != "webrtc" {
		t.Fatalf("expected tts audio to use webrtc transport, got %#v", ttsResult.Metadata)
	}
}

// TestClient_RelaySessionStartWebRTCAndReadLoop 验证高层 RelaySession 能完成 offer 转发、answer 等待和 TTS 结果分发。
func TestClient_RelaySessionStartWebRTCAndReadLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := newSDKClientWithGateway(t, gateway.New(gateway.Options{}))

	relay, err := client.ConnectRelay(ctx, sdk.WSConnectOptions{
		TenantID: "tenant-a",
		ClientID: "multiagent-relay",
	})
	if err != nil {
		t.Fatalf("connect relay: %v", err)
	}
	defer func() {
		_ = relay.Close()
	}()
	if relay.HelloAck().ResponseMode != sdk.WSResponseModeCompact {
		t.Fatalf("expected compact relay mode, got %#v", relay.HelloAck())
	}

	answer, err := relay.StartWebRTC(ctx, sdk.WebRTCOffer{
		CallID: "call-relay",
		UserID: "user-relay",
		SDP:    newTestAudioOffer(t),
	})
	if err != nil {
		t.Fatalf("start webrtc: %v", err)
	}
	if answer.CallID != "call-relay" || answer.SDP == "" {
		t.Fatalf("unexpected relay answer: %#v", answer)
	}

	if err := relay.SendICE(ctx, sdk.ICECandidate{CallID: "call-relay", UserID: "user-relay", Candidate: "candidate"}); err != nil {
		t.Fatalf("send ice: %v", err)
	}
	if err := relay.SendTTS(ctx, sdk.TTSCommand{CallID: "call-relay", UserID: "user-relay", Text: "请稍等", Voice: "101001", Language: "zh-CN"}); err != nil {
		t.Fatalf("send tts: %v", err)
	}

	errTTSSeen := errors.New("tts result seen")
	err = relay.ReadLoop(ctx, sdk.RelayHandlers{
		OnTTSResult: func(_ context.Context, result sdk.TTSResult) error {
			if result.CallID != "call-relay" || result.Text != "请稍等" {
				t.Fatalf("unexpected tts result: %#v", result)
			}
			if result.Metadata["audioTransport"] != "webrtc" {
				t.Fatalf("expected webrtc audio transport, got %#v", result.Metadata)
			}
			return errTTSSeen
		},
	})
	if !errors.Is(err, errTTSSeen) {
		t.Fatalf("expected read loop to stop on tts result, got %v", err)
	}
}

// readWSMessageType 读取到目标 WebSocket 消息类型，忽略中途的 ICE 等异步消息。
func readWSMessageType(ctx context.Context, t *testing.T, conn *sdk.WebSocket, typ string) (sdk.WSMessage, error) {
	t.Helper()
	for {
		message, err := conn.Read(ctx)
		if err != nil {
			return sdk.WSMessage{}, err
		}
		if message.Type == typ {
			return message, nil
		}
		t.Logf("skip websocket message while waiting for %s: %#v", typ, message)
	}
}

// newTestAudioOffer 创建带音频 m-line 的测试 WebRTC offer。
func newTestAudioOffer(t *testing.T) string {
	t.Helper()
	peer, err := pionwebrtc.NewPeerConnection(pionwebrtc.Configuration{})
	if err != nil {
		t.Fatalf("new test peer: %v", err)
	}
	t.Cleanup(func() {
		_ = peer.Close()
	})
	if _, err := peer.AddTransceiverFromKind(pionwebrtc.RTPCodecTypeAudio, pionwebrtc.RTPTransceiverInit{Direction: pionwebrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatalf("add audio transceiver: %v", err)
	}
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}
	return offer.SDP
}

// newSDKClient 创建连接到 httptest api-server 的 SDK client。
func newSDKClient(t *testing.T) *sdk.Client {
	t.Helper()
	return newSDKClientWithGateway(t, gateway.New(gateway.Options{}))
}

type sdkTestHarness struct {
	Client   *sdk.Client
	Registry *registry.Registry
	Config   *configcenter.Store
}

func newSDKHarness(t *testing.T) *sdkTestHarness {
	t.Helper()
	return newSDKHarnessWithGateway(t, gateway.New(gateway.Options{}))
}

// newSDKClientWithGateway 创建可注入 signaling gateway 的测试 SDK client。
func newSDKClientWithGateway(t *testing.T, gw *gateway.Gateway, options ...sdk.Option) *sdk.Client {
	t.Helper()
	return newSDKHarnessWithGateway(t, gw, options...).Client
}

// newSDKHarnessWithGateway 创建可注入 signaling gateway 的测试 SDK harness。
func newSDKHarnessWithGateway(t *testing.T, gw *gateway.Gateway, options ...sdk.Option) *sdkTestHarness {
	t.Helper()
	client := etcdutil.NewMemoryClient()
	reg := registry.New(client, registry.Options{})
	config := configcenter.New(client)
	api := httpapi.New(httpapi.Dependencies{
		Registry: reg,
		Router:   router.New(reg),
		Config:   config,
		Sessions: session.New(client),
		Gateway:  gw,
	})
	api.SetPBXControl(fakePBXControl{api: api})
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	options = append([]sdk.Option{sdk.WithHTTPClient(server.Client())}, options...)
	sdkClient, err := sdk.New(server.URL, options...)
	if err != nil {
		t.Fatalf("new sdk client: %v", err)
	}
	return &sdkTestHarness{Client: sdkClient, Registry: reg, Config: config}
}

type fakePBXControl struct {
	api *httpapi.API
}

func (f fakePBXControl) Send(ctx context.Context, message pbxprotocol.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch message.Type {
	case pbxprotocol.TypeWebRTCOffer:
		go f.api.HandlePBXMessage(context.Background(), pbxprotocol.Message{
			Type:         pbxprotocol.TypeWebRTCAnswer,
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			SDP:          "v=0\r\ns=fake-pbx-answer\r\n",
		})
	case pbxprotocol.TypeTTSCommand:
		go f.api.HandlePBXMessage(context.Background(), pbxprotocol.Message{
			Type:         pbxprotocol.TypeTTSResult,
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			UtteranceID:  message.UtteranceID,
			Text:         message.Text,
			Voice:        message.Voice,
			Language:     message.Language,
			Format:       "pcmu",
			SampleRate:   8000,
			IsLast:       true,
			Metadata:     map[string]string{"audioTransport": "webrtc"},
		})
	}
	return nil
}

