//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client

import (
	"context"
	"errors"
	"fmt"
)

// ErrRelayMessageIgnored 表示当前消息不是调用方正在等待的目标消息。
var ErrRelayMessageIgnored = errors.New("relay message ignored")

// RelaySession 是业务主服务接入 PBX 的高层 WebSocket 会话。
type RelaySession struct {
	ws       *WebSocket
	helloAck WSMessage
}

// WebRTCOffer 是业务主服务转发给 PBX 的用户侧 WebRTC offer。
type WebRTCOffer struct {
	CallID   string
	UserID   string
	SDP      string
	Metadata map[string]string
}

// WebRTCAnswer 是 PBX 返回给业务主服务并需要继续转发给用户侧的 WebRTC answer。
type WebRTCAnswer struct {
	CallID string
	UserID string
	SDP    string
}

// ICECandidate 是业务主服务和 PBX 之间互相转发的 ICE candidate。
type ICECandidate struct {
	CallID    string
	UserID    string
	Candidate string
}

// ASRCallback 是 PBX 识别出用户语音后返回给业务主服务的文本回调。
type ASRCallback struct {
	CallID     string
	UserID     string
	Text       string
	IsFinal    bool
	Confidence float64
	Language   string
	Metadata   map[string]string
}

// TTSCommand 是业务主服务要求 PBX 合成并通过 WebRTC 下行播放的文本命令。
type TTSCommand struct {
	CallID   string
	UserID   string
	Text     string
	Voice    string
	Language string
	Metadata map[string]string
}

// TTSResult 是 PBX 接受 TTS 命令后的业务确认；音频本体通过 WebRTC 返回给用户。
type TTSResult struct {
	CallID     string
	UserID     string
	Text       string
	Format     string
	SampleRate int
	IsLast     bool
	Voice      string
	Language   string
	Metadata   map[string]string
}

// RelayHandlers 汇总 PBX 回传消息的处理函数，供业务主服务在 ReadLoop 中按类型消费。
type RelayHandlers struct {
	OnAnswer    func(context.Context, WebRTCAnswer) error
	OnICE       func(context.Context, ICECandidate) error
	OnASRResult func(context.Context, ASRCallback) error
	OnTTSResult func(context.Context, TTSResult) error
	OnError     func(context.Context, WSMessage) error
	OnMessage   func(context.Context, WSMessage) error
}

// ConnectRelay 以 compact 模式建立业务主服务到 PBX 的控制连接，适合 go-multiagent 转发信令和接收 ASR 回调。
func (c *Client) ConnectRelay(ctx context.Context, options WSConnectOptions) (*RelaySession, error) {
	if options.ResponseMode == "" {
		options.ResponseMode = WSResponseModeCompact
	}
	ws, ack, err := c.ConnectWebSocket(ctx, options)
	if err != nil {
		return nil, err
	}
	return &RelaySession{ws: ws, helloAck: ack}, nil
}

// ConnectionID 返回 PBX 分配给当前 relay 控制连接的 ID。
func (s *RelaySession) ConnectionID() string {
	if s == nil || s.ws == nil {
		return ""
	}
	return s.ws.ConnectionID()
}

// HelloAck 返回 relay 建连阶段 PBX 返回的确认消息。
func (s *RelaySession) HelloAck() WSMessage {
	if s == nil {
		return WSMessage{}
	}
	return s.helloAck
}

// WebSocket 返回底层 WebSocket，供需要自定义协议细节的接入方使用。
func (s *RelaySession) WebSocket() *WebSocket {
	if s == nil {
		return nil
	}
	return s.ws
}

// Close 关闭 relay 控制连接。
func (s *RelaySession) Close() error {
	if s == nil || s.ws == nil {
		return nil
	}
	return s.ws.Close()
}

// StartWebRTC 转发用户 offer 到 PBX，并同步等待同一通话的 answer。
func (s *RelaySession) StartWebRTC(ctx context.Context, offer WebRTCOffer) (WebRTCAnswer, error) {
	if err := s.ForwardWebRTCOffer(ctx, offer); err != nil {
		return WebRTCAnswer{}, err
	}
	return s.WaitForWebRTCAnswer(ctx, offer.CallID, offer.UserID)
}

// ForwardWebRTCOffer 将用户侧 WebRTC offer 转发给 PBX，answer 可由 WaitForWebRTCAnswer 或 ReadLoop 消费。
func (s *RelaySession) ForwardWebRTCOffer(ctx context.Context, offer WebRTCOffer) error {
	if s == nil || s.ws == nil {
		return ErrWebSocketClosed
	}
	metadata := map[string]string{"media": "audio", "source": "simulspeak-sdk-relay"}
	for key, value := range offer.Metadata {
		metadata[key] = value
	}
	return s.ws.Send(ctx, WSMessage{
		Type:      "webrtc_offer",
		RequestID: requestID("offer"),
		CallID:    offer.CallID,
		UserID:    offer.UserID,
		SDP:       offer.SDP,
		Metadata:  metadata,
	})
}

// WaitForWebRTCAnswer 持续读取 PBX 消息，直到拿到指定 call/user 的 WebRTC answer。
func (s *RelaySession) WaitForWebRTCAnswer(ctx context.Context, callID, userID string) (WebRTCAnswer, error) {
	for {
		message, err := s.Read(ctx)
		if err != nil {
			return WebRTCAnswer{}, err
		}
		answer, err := webRTCAnswerFromMessage(message, callID, userID)
		if err == nil {
			return answer, nil
		}
		if !errors.Is(err, ErrRelayMessageIgnored) {
			return WebRTCAnswer{}, err
		}
	}
}

// SendICE 将用户侧 ICE candidate 转发给 PBX。
func (s *RelaySession) SendICE(ctx context.Context, candidate ICECandidate) error {
	if s == nil || s.ws == nil {
		return ErrWebSocketClosed
	}
	return s.ws.SendICE(ctx, candidate.CallID, candidate.UserID, candidate.Candidate)
}

// SendTTS 请求 PBX 调用 TTS，并通过已经建立的 WebRTC 连接向用户播放音频。
func (s *RelaySession) SendTTS(ctx context.Context, command TTSCommand) error {
	if s == nil || s.ws == nil {
		return ErrWebSocketClosed
	}
	return s.ws.Send(ctx, WSMessage{
		Type:      "tts_command",
		RequestID: requestID("tts"),
		CallID:    command.CallID,
		UserID:    command.UserID,
		Text:      command.Text,
		Voice:     command.Voice,
		Language:  command.Language,
		Metadata:  cloneStringMap(command.Metadata),
	})
}

// Read 读取一条 PBX WebSocket 原始消息，便于接入方做自定义分发。
func (s *RelaySession) Read(ctx context.Context) (WSMessage, error) {
	if s == nil || s.ws == nil {
		return WSMessage{}, ErrWebSocketClosed
	}
	return s.ws.Read(ctx)
}

// ReadLoop 持续读取 PBX 回包，并按消息类型调用对应 handler。
func (s *RelaySession) ReadLoop(ctx context.Context, handlers RelayHandlers) error {
	for {
		message, err := s.Read(ctx)
		if err != nil {
			return err
		}
		if err := dispatchRelayMessage(ctx, handlers, message); err != nil {
			return err
		}
	}
}

// dispatchRelayMessage 将单条 PBX 消息转换成更明确的 SDK 事件并回调给业务主服务。
func dispatchRelayMessage(ctx context.Context, handlers RelayHandlers, message WSMessage) error {
	if handlers.OnMessage != nil {
		if err := handlers.OnMessage(ctx, message); err != nil {
			return err
		}
	}
	switch message.Type {
	case "webrtc_answer":
		if handlers.OnAnswer != nil {
			return handlers.OnAnswer(ctx, WebRTCAnswer{CallID: message.CallID, UserID: message.UserID, SDP: message.SDP})
		}
	case "ice":
		if handlers.OnICE != nil {
			return handlers.OnICE(ctx, ICECandidate{CallID: message.CallID, UserID: message.UserID, Candidate: message.Candidate})
		}
	case "asr_result":
		if handlers.OnASRResult != nil {
			return handlers.OnASRResult(ctx, ASRCallback{
				CallID:     message.CallID,
				UserID:     message.UserID,
				Text:       message.Text,
				IsFinal:    message.IsFinal,
				Confidence: message.Confidence,
				Language:   message.Language,
				Metadata:   cloneStringMap(message.Metadata),
			})
		}
	case "tts_result":
		if handlers.OnTTSResult != nil {
			return handlers.OnTTSResult(ctx, TTSResult{
				CallID:     message.CallID,
				UserID:     message.UserID,
				Text:       message.Text,
				Format:     message.Format,
				SampleRate: message.SampleRate,
				IsLast:     message.IsLast,
				Voice:      message.Voice,
				Language:   message.Language,
				Metadata:   cloneStringMap(message.Metadata),
			})
		}
	case "error":
		if handlers.OnError != nil {
			return handlers.OnError(ctx, message)
		}
		return fmt.Errorf("pbx relay error: %s", message.Error)
	}
	return nil
}

// webRTCAnswerFromMessage 校验消息是否为调用方正在等待的 WebRTC answer。
func webRTCAnswerFromMessage(message WSMessage, callID, userID string) (WebRTCAnswer, error) {
	if message.Type == "error" {
		return WebRTCAnswer{}, fmt.Errorf("pbx relay error: %s", message.Error)
	}
	if message.Type != "webrtc_answer" {
		return WebRTCAnswer{}, ErrRelayMessageIgnored
	}
	if callID != "" && message.CallID != callID {
		return WebRTCAnswer{}, ErrRelayMessageIgnored
	}
	if userID != "" && message.UserID != userID {
		return WebRTCAnswer{}, ErrRelayMessageIgnored
	}
	return WebRTCAnswer{CallID: message.CallID, UserID: message.UserID, SDP: message.SDP}, nil
}

