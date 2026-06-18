// PBX消息桥接：处理PBX控制通道消息→ 转发到前端WebSocket
package httpapi

import (
	"context"
	"log/slog"
	"strings"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
)

// registerFrontendConn 注册前端 WebSocket 连接，建立 connectionID → wsConn 的映射。
func (api *API) registerFrontendConn(connectionID string, conn *wsConn) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.frontConns[connectionID] = conn
}

// firstBridgeValue 返回第一个非空字符串值，用于从多个可能的来源提取字段（如 provider 名称）。
func firstBridgeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (api *API) unregisterFrontendConn(connectionID string) {
	api.mu.Lock()
	session := api.interpreters[connectionID]
	delete(api.frontConns, connectionID)
	delete(api.interpreters, connectionID)
	api.mu.Unlock()
	if session != nil {
		session.stopTTSCoordinator()
	}
}

func (api *API) endInterpreterSession(ctx context.Context, connectionID string) {
	api.mu.RLock()
	session := api.interpreters[connectionID]
	api.mu.RUnlock()
	if session == nil {
		return
	}
	session.endStore(ctx)
}

func (api *API) frontendConn(connectionID string) *wsConn {
	api.mu.RLock()
	defer api.mu.RUnlock()
	return api.frontConns[connectionID]
}

// HandlePBXMessage 处理来自 PBX 控制通道的消息，按消息类型分发给对应的处理函数：
// WebRTC answer → 转发给前端、ICE → 转发、ASR结果 → 触发同传处理、翻译结果 → 转发+LLM纠错、TTS结果 → 转发、错误 → 转发。
// 这是 PBX→前端消息桥接的核心入口。
func (api *API) HandlePBXMessage(ctx context.Context, message pbxprotocol.Message) {
	conn := api.frontendConn(message.ConnectionID)
	if conn == nil {
		slog.WarnContext(ctx, "收到 PBX control 消息但前端连接不存在",
			slog.String("connectionId", message.ConnectionID),
			slog.String("type", message.Type),
		)
		return
	}
	switch message.Type {
	case pbxprotocol.TypeWebRTCAnswer:
		_ = writeLoggedWebSocketJSON(ctx, conn, message.ConnectionID, wsMessage{
			Type:         "webrtc_answer",
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			SDP:          message.SDP,
		})
	case pbxprotocol.TypeICE:
		_ = writeLoggedWebSocketJSON(ctx, conn, message.ConnectionID, wsMessage{
			Type:         "ice",
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			Candidate:    message.Candidate,
		})
	case pbxprotocol.TypeASRResult:
		api.handlePBXASRResult(ctx, conn, message)
	case pbxprotocol.TypeTranslationResult:
		api.handlePBXTranslationResult(ctx, conn, message)
	case pbxprotocol.TypeTTSResult:
		_ = writeLoggedWebSocketJSON(ctx, conn, message.ConnectionID, wsMessage{
			Type:         "tts_result",
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			UtteranceID:  message.UtteranceID,
			Text:         message.Text,
			Format:       message.Format,
			SampleRate:   message.SampleRate,
			IsLast:       message.IsLast,
			Sequence:     message.Sequence,
			Voice:        message.Voice,
			Language:     message.Language,
			Metadata:     message.Metadata,
		})
	case pbxprotocol.TypeRecordingResult:
		_ = writeLoggedWebSocketJSON(ctx, conn, message.ConnectionID, wsMessage{
			Type:         "recording_result",
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			Format:       message.Format,
			SampleRate:   message.SampleRate,
			IsLast:       message.IsLast,
			Metadata:     message.Metadata,
		})
	case pbxprotocol.TypeError:
		_ = writeLoggedWebSocketJSON(ctx, conn, message.ConnectionID, wsMessage{
			Type:         "error",
			RequestID:    message.RequestID,
			ConnectionID: message.ConnectionID,
			CallID:       message.CallID,
			UserID:       message.UserID,
			Error:        message.Error,
		})
	}
}

func (api *API) handlePBXASRResult(ctx context.Context, conn *wsConn, message pbxprotocol.Message) {
	if session := api.interpreter(message.ConnectionID); session != nil {
		session.ensureProviderSummary(ctx, "asr", message.Metadata["provider"])
	}
	result := model.ASRResult{
		CallID:      message.CallID,
		UtteranceID: message.UtteranceID,
		Text:        message.Text,
		IsFinal:     message.IsFinal,
		Confidence:  message.Confidence,
		Language:    message.Language,
	}
	api.processASRResult(ctx, conn, message.ConnectionID, message.UserID, result)
}

func (api *API) handlePBXTranslationResult(ctx context.Context, conn *wsConn, message pbxprotocol.Message) {
	if session := api.interpreter(message.ConnectionID); session != nil {
		session.ensureProviderSummary(ctx, "tmt", firstBridgeValue(message.Metadata["provider"], message.Engine))
	}
	result := model.TranslationResult{
		CallID:      message.CallID,
		UtteranceID: message.UtteranceID,
		SourceText:  message.SourceText,
		Text:        message.Text,
		IsFinal:     message.IsFinal,
		Engine:      message.Engine,
		Revised:     message.Revised,
		Language:    message.Language,
	}
	api.processTranslationResult(ctx, conn, message.ConnectionID, message.UserID, result)
}
