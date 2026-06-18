// 前端WebSocket处理：client_hello→ offer→ ICE→ set_strategy消息路由
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
)

const (
	wsResponseModeDebug   = "debug"
	wsResponseModeCompact = "compact"
)

type wsMessage struct {
	Type            string                                        `json:"type"`
	RequestID       string                                        `json:"requestId,omitempty"`
	ConnectionID    string                                        `json:"connectionId,omitempty"`
	TenantID        string                                        `json:"tenantId,omitempty"`
	Extension       string                                        `json:"extension,omitempty"`
	ClientID        string                                        `json:"clientId,omitempty"`
	ResponseMode    string                                        `json:"responseMode,omitempty"`
	CallID          string                                        `json:"callId,omitempty"`
	UserID          string                                        `json:"userId,omitempty"`
	SDP             string                                        `json:"sdp,omitempty"`
	Candidate       string                                        `json:"candidate,omitempty"`
	Text            string                                        `json:"text,omitempty"`
	UtteranceID     string                                        `json:"utteranceId,omitempty"`
	SourceText      string                                        `json:"sourceText,omitempty"`
	Engine          string                                        `json:"engine,omitempty"`
	Revised         bool                                          `json:"revised"`
	Audio           string                                        `json:"audio,omitempty"`
	Format          string                                        `json:"format,omitempty"`
	SampleRate      int                                           `json:"sampleRate,omitempty"`
	Sequence        int                                           `json:"sequence,omitempty"`
	IsLast          bool                                          `json:"isLast,omitempty"`
	IsFinal         bool                                          `json:"isFinal,omitempty"`
	Confidence      float64                                       `json:"confidence,omitempty"`
	Voice           string                                        `json:"voice,omitempty"`
	Language        string                                        `json:"language,omitempty"`
	Metadata        map[string]string                             `json:"metadata,omitempty"`
	ProviderConfigs map[model.CapabilityType]model.ProviderConfig `json:"providerConfigs,omitempty"`
	Error           string                                        `json:"error,omitempty"`
}

type wsConn struct {
	netConn         net.Conn
	reader          *bufio.Reader
	writer          *bufio.Writer
	mu              sync.Mutex
	stateMu         sync.RWMutex
	responseMode    string
	strategy        string
	dubbing         bool
	languageOptions model.SessionLanguageOptions
}

func (c *wsConn) setInterpreterOptions(metadata map[string]string) (map[string]string, error) {
	languageOptions, err := model.NormalizeSessionLanguageOptions(metadata)
	if err != nil {
		return nil, err
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	requested := strings.TrimSpace(metadata["translateStrategy"])
	if requested == "" {
		requested = "tmt"
	}
	c.strategy = effectiveTranslateStrategy(requested)
	c.dubbing = truthyMetadata(metadata["dubbing"])
	c.languageOptions = languageOptions
	response := languageOptions.Metadata()
	response["translateStrategy"] = c.strategy
	response["requestedTranslateStrategy"] = requested
	response["dubbing"] = boolMetadata(c.dubbing)
	return response, nil
}

func (c *wsConn) currentLanguageOptions() model.SessionLanguageOptions {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	if c.languageOptions.SourceLanguage == "" {
		return model.DefaultSessionLanguageOptions()
	}
	return c.languageOptions.WithDefaults()
}

func (c *wsConn) languageMetadata(base map[string]string) map[string]string {
	out := cloneWSMetadata(base)
	for key, value := range c.currentLanguageOptions().Metadata() {
		out[key] = value
	}
	return out
}

func cloneWSMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (c *wsConn) setStrategy(strategy string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.strategy = effectiveTranslateStrategy(strategy)
	return c.strategy
}

func (c *wsConn) currentStrategy() string {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return effectiveTranslateStrategy(c.strategy)
}

func (c *wsConn) setDubbing(value string) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.dubbing = truthyMetadata(value)
	return c.dubbing
}

func (c *wsConn) dubbingEnabled() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.dubbing
}

// handleWebSocket 处理 PBX 客户端 WebSocket 连接，完成 presence 上线和 provider 参数上报。
func (api *API) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.URL.Query().Get("token")
	connection, err := api.Gateway.Connect(ctx, token)
	if err != nil {
		logWebSocketWarning(ctx, "WebSocket 连接被拒绝", "", err)
		JSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	logWebSocketRegistered(ctx, connection.ID, connection.TenantID, connection.Extension, r.RemoteAddr)

	conn, err := upgradeWebSocket(w, r)
	if err != nil {
		logWebSocketWarning(ctx, "WebSocket 升级失败", connection.ID, err)
		_ = api.Gateway.Disconnect(ctx, connection.ID)
		return
	}
	connectedAt := time.Now()
	logWebSocketConnected(ctx, connection.ID, connection.TenantID, connection.Extension, r.RemoteAddr)
	api.registerFrontendConn(connection.ID, conn)
	defer func() {
		logWebSocketDisconnected(ctx, connection.ID, connectedAt)
		if control := api.pbxControl(); control != nil {
			_ = control.Send(context.Background(), controlMessageCloseSession(connection.ID))
		}
		api.unbindPBXControl(connection.ID)
		api.endInterpreterSession(context.Background(), connection.ID)
		api.unregisterFrontendConn(connection.ID)
		_ = api.Gateway.Disconnect(ctx, connection.ID)
		_ = conn.netConn.Close()
	}()

	_ = writeLoggedWebSocketJSON(ctx, conn, connection.ID, wsMessage{
		Type:         "connected",
		ConnectionID: connection.ID,
		TenantID:     connection.TenantID,
		Extension:    connection.Extension,
		ClientID:     connection.Extension,
	})

	for {
		opcode, payload, err := readWebSocketFrame(conn.reader, true)
		if err != nil {
			logWebSocketReceiveError(ctx, connection.ID, err)
			return
		}
		switch opcode {
		case 0x1:
			api.handleWebSocketMessage(ctx, conn, connection.ID, payload)
		case 0x8:
			logWebSocketControl(ctx, connection.ID, "close", len(payload))
			_ = writeWebSocketClose(conn)
			return
		case 0x9:
			logWebSocketControl(ctx, connection.ID, "ping", len(payload))
			conn.mu.Lock()
			_ = writeWebSocketFrame(conn.writer, 0xA, payload, false)
			conn.mu.Unlock()
		default:
			logWebSocketControl(ctx, connection.ID, fmt.Sprintf("opcode_%d", opcode), len(payload))
		}
	}
}

// handleWebSocketMessage 处理客户端 JSON 文本帧，支持业务服务注册、WebRTC 信令转发和 TTS 命令。
func (api *API) handleWebSocketMessage(ctx context.Context, conn *wsConn, connectionID string, payload []byte) {
	var message wsMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		logWebSocketWarning(ctx, "收到非法 WebSocket JSON", connectionID, err)
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", Error: "invalid json"})
		return
	}
	logWebSocketMessageReceived(ctx, connectionID, message, len(payload))

	switch message.Type {
	case "client_hello":
		conn.setResponseMode(message.ResponseMode)
		interpreterMetadata, err := conn.setInterpreterOptions(message.Metadata)
		if err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, Error: err.Error()})
			return
		}
		responseMode := conn.currentResponseMode()
		providerConfigs := model.CloneProviderConfigs(message.ProviderConfigs)
		if err := api.Gateway.SetProviderConfigs(connectionID, providerConfigs); err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, Error: err.Error()})
			return
		}
		ack := wsMessage{
			Type:            "client_hello_ack",
			RequestID:       message.RequestID,
			ConnectionID:    connectionID,
			TenantID:        message.TenantID,
			Extension:       message.Extension,
			ClientID:        message.ClientID,
			ResponseMode:    responseMode,
			Metadata:        interpreterMetadata,
			ProviderConfigs: redactProviderConfigMap(providerConfigs),
		}
		if responseMode == wsResponseModeCompact {
			ack = wsMessage{
				Type:         "client_hello_ack",
				RequestID:    message.RequestID,
				ConnectionID: connectionID,
				ResponseMode: responseMode,
				Metadata:     interpreterMetadata,
			}
		}
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, ack)
	case "webrtc_offer":
		logWebRTCOfferReceived(ctx, connectionID, message)
		if err := api.Gateway.SendOffer(connContext(), connectionID, message.SDP); err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, Error: err.Error()})
			return
		}
		_ = api.Gateway.BindCall(connectionID, message.CallID)
		pbxNodeID, err := api.bindPBXControl(ctx, connectionID, message.CallID)
		if err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, ConnectionID: connectionID, CallID: message.CallID, UserID: message.UserID, Error: err.Error()})
			return
		}
		gatewayConn, _ := api.Gateway.GetConnection(connectionID)
		if message.TenantID == "" {
			message.TenantID = gatewayConn.TenantID
		}
		message.Metadata = conn.languageMetadata(message.Metadata)
		api.createInterpreterSession(ctx, connectionID, message.CallID, message.UserID, gatewayConn.TenantID, conn.currentStrategy(), conn.dubbingEnabled(), conn.currentLanguageOptions())
		if !conn.compactResponseMode() {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
				Type:         "webrtc_offer_ack",
				RequestID:    message.RequestID,
				ConnectionID: connectionID,
				CallID:       message.CallID,
				UserID:       message.UserID,
				Metadata: map[string]string{
					"pbxNodeId": pbxNodeID,
				},
			})
		}
		control := api.pbxControl()
		if control == nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, ConnectionID: connectionID, CallID: message.CallID, UserID: message.UserID, Error: "pbx control is not configured"})
			return
		}
		if err := control.Send(ctx, controlMessageFromWS("webrtc_offer", connectionID, message)); err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, ConnectionID: connectionID, CallID: message.CallID, UserID: message.UserID, Error: err.Error()})
		}
	case "ice":
		logWebRTCICEReceived(ctx, connectionID, message)
		if err := api.Gateway.SendICE(connContext(), connectionID, message.Candidate); err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, Error: err.Error()})
			return
		}
		if control := api.pbxControl(); control != nil {
			if err := control.Send(ctx, controlMessageFromWS("ice", connectionID, message)); err != nil {
				logWebSocketWarning(ctx, "转发 WebRTC ICE 到 PBX 失败", connectionID, err)
			}
		}
		if !conn.compactResponseMode() {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
				Type:         "ice_ack",
				RequestID:    message.RequestID,
				ConnectionID: connectionID,
				CallID:       message.CallID,
				UserID:       message.UserID,
			})
		}
	case "tts_command":
		logTTSCommandReceived(ctx, connectionID, message)
		if !conn.compactResponseMode() {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
				Type:         "tts_command_ack",
				RequestID:    message.RequestID,
				ConnectionID: connectionID,
				CallID:       message.CallID,
				Text:         message.Text,
				Voice:        message.Voice,
				Language:     message.Language,
			})
		}
		control := api.pbxControl()
		if control == nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, ConnectionID: connectionID, CallID: message.CallID, UserID: message.UserID, Error: "pbx control is not configured"})
			return
		}
		if err := control.Send(ctx, controlMessageFromWS("tts_command", connectionID, message)); err != nil {
			_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, ConnectionID: connectionID, CallID: message.CallID, UserID: message.UserID, Error: err.Error()})
		}
	case "set_strategy":
		strategy := conn.setStrategy(message.Metadata["translateStrategy"])
		if session := api.interpreter(connectionID); session != nil {
			session.mu.Lock()
			session.strategy = strategy
			session.mu.Unlock()
		}
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
			Type:         "set_strategy_ack",
			RequestID:    message.RequestID,
			ConnectionID: connectionID,
			Metadata: map[string]string{
				"translateStrategy": strategy,
			},
		})
	case "set_dubbing":
		dubbing := conn.setDubbing(message.Metadata["dubbing"])
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
			Type:         "set_dubbing_ack",
			RequestID:    message.RequestID,
			ConnectionID: connectionID,
			Metadata: map[string]string{
				"dubbing": boolMetadata(dubbing),
			},
		})
	case "ping":
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "pong", RequestID: message.RequestID, ConnectionID: connectionID})
	default:
		_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{Type: "error", RequestID: message.RequestID, Error: "unknown message type"})
	}
}

func controlMessageCloseSession(connectionID string) pbxprotocol.Message {
	return pbxprotocol.Message{Type: pbxprotocol.TypeCloseSession, ConnectionID: connectionID}
}

func controlMessageFromWS(typ, connectionID string, message wsMessage) pbxprotocol.Message {
	return pbxprotocol.Message{
		Type:         typ,
		RequestID:    message.RequestID,
		ConnectionID: connectionID,
		TenantID:     message.TenantID,
		CallID:       message.CallID,
		UserID:       message.UserID,
		SDP:          message.SDP,
		Candidate:    message.Candidate,
		Text:         message.Text,
		UtteranceID:  message.UtteranceID,
		Voice:        message.Voice,
		Language:     message.Language,
		Metadata:     message.Metadata,
	}
}

// connContext 返回 WebSocket 消息处理使用的短生命周期上下文。
func connContext() context.Context {
	return context.Background()
}

// setResponseMode 保存当前 WebSocket 连接期望的服务端回包模式。
func (c *wsConn) setResponseMode(mode string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.responseMode = normalizeWSResponseMode(mode)
}

// currentResponseMode 返回当前连接回包模式，未设置时使用 debug 兼容模式。
func (c *wsConn) currentResponseMode() string {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return normalizeWSResponseMode(c.responseMode)
}

// compactResponseMode 判断当前连接是否启用了收敛回包模式。
func (c *wsConn) compactResponseMode() bool {
	return c.currentResponseMode() == wsResponseModeCompact
}

func effectiveTranslateStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "hybrid", "deepseek", "llm":
		return strings.ToLower(strings.TrimSpace(strategy))
	default:
		return "tmt"
	}
}

func truthyMetadata(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func boolMetadata(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// normalizeWSResponseMode 归一化回包模式，避免客户端传入大小写或空值造成歧义。
func normalizeWSResponseMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), wsResponseModeCompact) {
		return wsResponseModeCompact
	}
	return wsResponseModeDebug
}
