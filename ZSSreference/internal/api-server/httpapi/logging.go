//  HTTP请求日志中间件
package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

type requestLogResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	hijack bool
}

// WriteHeader 记录 HTTP 状态码后继续写给底层 ResponseWriter。
func (w *requestLogResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write 记录响应字节数；未显式设置状态码时按 200 处理。
func (w *requestLogResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

// Flush 透传 SSE 等流式响应需要的 flush 能力。
func (w *requestLogResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 透传 WebSocket upgrade 需要的 hijack 能力，并将状态记录为 101。
func (w *requestLogResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijack")
	}
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	w.hijack = true
	return hijacker.Hijack()
}

// statusCode 返回最终 HTTP 状态码。
func (w *requestLogResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// logHTTPRequest 打印每个 HTTP 请求的访问日志。
func logHTTPRequest(r *http.Request, w *requestLogResponseWriter, started time.Time) {
	duration := time.Since(started)
	slog.InfoContext(r.Context(), "HTTP 请求",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", w.statusCode()),
		slog.Int("bytes", w.bytes),
		slog.Bool("hijacked", w.hijack),
		slog.Duration("duration", duration),
		slog.String("remoteAddr", r.RemoteAddr),
		slog.String("userAgent", r.UserAgent()),
	)
}

// logWebSocketRegistered 打印 WebSocket token 通过网关注册后的日志。
func logWebSocketRegistered(ctx context.Context, connectionID, tenantID, extension, remoteAddr string) {
	slog.InfoContext(ctx, "WebSocket 连接已注册",
		slog.String("connectionId", connectionID),
		slog.String("tenantId", tenantID),
		slog.String("extension", extension),
		slog.String("remoteAddr", remoteAddr),
	)
}

// logWebSocketConnected 打印 WebSocket upgrade 成功后的日志。
func logWebSocketConnected(ctx context.Context, connectionID, tenantID, extension, remoteAddr string) {
	slog.InfoContext(ctx, "WebSocket 已连接",
		slog.String("connectionId", connectionID),
		slog.String("tenantId", tenantID),
		slog.String("extension", extension),
		slog.String("remoteAddr", remoteAddr),
	)
}

// logWebSocketDisconnected 打印 WebSocket 断开日志。
func logWebSocketDisconnected(ctx context.Context, connectionID string, connectedAt time.Time) {
	slog.InfoContext(ctx, "WebSocket 已断开",
		slog.String("connectionId", connectionID),
		slog.Duration("duration", time.Since(connectedAt)),
	)
}

// logWebSocketReceiveError 打印 WebSocket 读取停止或失败日志。
func logWebSocketReceiveError(ctx context.Context, connectionID string, err error) {
	slog.InfoContext(ctx, "WebSocket 读取已停止",
		slog.String("connectionId", connectionID),
		slog.String("error", err.Error()),
	)
}

// logWebSocketWarning 打印 WebSocket 异常路径日志。
func logWebSocketWarning(ctx context.Context, message, connectionID string, err error) {
	slog.WarnContext(ctx, message,
		slog.String("connectionId", connectionID),
		slog.String("error", err.Error()),
	)
}

// writeLoggedWebSocketJSON 发送 WebSocket JSON 消息并打印发送结果。
func writeLoggedWebSocketJSON(ctx context.Context, conn *wsConn, connectionID string, message wsMessage) error {
	err := writeWebSocketJSON(conn, message)
	if err != nil {
		logWebSocketWarning(ctx, "WebSocket 消息发送失败", connectionID, err)
		return err
	}
	logWebSocketMessageSent(ctx, connectionID, message)
	return nil
}

// logWebSocketControl 打印 ping/close 等控制帧日志。
func logWebSocketControl(ctx context.Context, connectionID, frame string, payloadBytes int) {
	slog.InfoContext(ctx, "WebSocket 控制帧",
		slog.String("connectionId", connectionID),
		slog.String("frame", frame),
		slog.Int("payloadBytes", payloadBytes),
	)
}

// logWebSocketMessageReceived 打印收到的 WebSocket JSON 消息摘要。
func logWebSocketMessageReceived(ctx context.Context, connectionID string, message wsMessage, payloadBytes int) {
	slog.LogAttrs(ctx, slog.LevelInfo, "收到 WebSocket 消息", websocketMessageAttrs(connectionID, message, payloadBytes)...)
}

// logWebSocketMessageSent 打印发出的 WebSocket JSON 消息摘要。
func logWebSocketMessageSent(ctx context.Context, connectionID string, message wsMessage) {
	slog.LogAttrs(ctx, slog.LevelInfo, "已发送 WebSocket 消息", websocketMessageAttrs(connectionID, message, 0)...)
}

// logWebRTCOfferReceived 打印 WebRTC offer 进入 PBX 控制链路的日志。
func logWebRTCOfferReceived(ctx context.Context, connectionID string, message wsMessage) {
	slog.InfoContext(ctx, "收到 WebRTC offer",
		slog.String("connectionId", connectionID),
		slog.String("requestId", message.RequestID),
		slog.String("callId", message.CallID),
		slog.String("userId", message.UserID),
		slog.Int("sdpBytes", len(message.SDP)),
		slog.String("sdpPreview", truncateLogValue(message.SDP, 120)),
	)
}

// logWebRTCICEReceived 打印 WebRTC ICE candidate 进入 PBX 控制链路的日志。
func logWebRTCICEReceived(ctx context.Context, connectionID string, message wsMessage) {
	slog.InfoContext(ctx, "收到 WebRTC ICE",
		slog.String("connectionId", connectionID),
		slog.String("requestId", message.RequestID),
		slog.String("callId", message.CallID),
		slog.String("userId", message.UserID),
		slog.Int("candidateBytes", len(message.Candidate)),
		slog.String("candidatePreview", truncateLogValue(message.Candidate, 160)),
	)
}

// logTTSCommandReceived 打印 TTS 命令进入 PBX 控制链路的日志。
func logTTSCommandReceived(ctx context.Context, connectionID string, message wsMessage) {
	slog.InfoContext(ctx, "收到 TTS 命令",
		slog.String("connectionId", connectionID),
		slog.String("requestId", message.RequestID),
		slog.String("callId", message.CallID),
		slog.String("userId", message.UserID),
		slog.String("voice", message.Voice),
		slog.String("language", message.Language),
		slog.Int("textBytes", len(message.Text)),
		slog.String("textPreview", truncateLogValue(message.Text, 80)),
	)
}

// websocketMessageAttrs 构造安全的 WebSocket 消息日志字段，避免输出 provider secrets 和完整 SDP。
func websocketMessageAttrs(connectionID string, message wsMessage, payloadBytes int) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("connectionId", connectionID),
		slog.String("type", message.Type),
		slog.String("requestId", message.RequestID),
		slog.String("callId", message.CallID),
		slog.String("userId", message.UserID),
	}
	if payloadBytes > 0 {
		attrs = append(attrs, slog.Int("payloadBytes", payloadBytes))
	}
	if message.TenantID != "" {
		attrs = append(attrs, slog.String("tenantId", message.TenantID))
	}
	if message.ClientID != "" {
		attrs = append(attrs, slog.String("clientId", message.ClientID))
	}
	if message.Extension != "" {
		attrs = append(attrs, slog.String("extension", message.Extension))
	}
	if message.SDP != "" {
		attrs = append(attrs, slog.Int("sdpBytes", len(message.SDP)), slog.String("sdpPreview", truncateLogValue(message.SDP, 120)))
	}
	if message.Candidate != "" {
		attrs = append(attrs, slog.Int("candidateBytes", len(message.Candidate)), slog.String("candidatePreview", truncateLogValue(message.Candidate, 160)))
	}
	if message.Text != "" {
		attrs = append(attrs, slog.Int("textBytes", len(message.Text)), slog.String("textPreview", truncateLogValue(message.Text, 80)))
	}
	if message.UtteranceID != "" {
		attrs = append(attrs, slog.String("utteranceId", message.UtteranceID))
	}
	if message.SourceText != "" {
		attrs = append(attrs, slog.Int("sourceTextBytes", len(message.SourceText)), slog.String("sourceTextPreview", truncateLogValue(message.SourceText, 80)))
	}
	if message.Engine != "" {
		attrs = append(attrs, slog.String("engine", message.Engine))
	}
	if message.Revised {
		attrs = append(attrs, slog.Bool("revised", message.Revised))
	}
	if message.Audio != "" {
		attrs = append(attrs, slog.Int("audioBase64Bytes", len(message.Audio)))
	}
	if message.Format != "" {
		attrs = append(attrs, slog.String("format", message.Format))
	}
	if message.SampleRate != 0 {
		attrs = append(attrs, slog.Int("sampleRate", message.SampleRate))
	}
	if message.Sequence != 0 {
		attrs = append(attrs, slog.Int("sequence", message.Sequence))
	}
	if message.IsLast {
		attrs = append(attrs, slog.Bool("isLast", message.IsLast))
	}
	if message.IsFinal {
		attrs = append(attrs, slog.Bool("isFinal", message.IsFinal))
	}
	if message.Confidence != 0 {
		attrs = append(attrs, slog.Float64("confidence", message.Confidence))
	}
	if message.Voice != "" {
		attrs = append(attrs, slog.String("voice", message.Voice))
	}
	if message.Language != "" {
		attrs = append(attrs, slog.String("language", message.Language))
	}
	if len(message.ProviderConfigs) > 0 {
		attrs = append(attrs, slog.Any("providers", providerLogSummary(message)))
	}
	return attrs
}

// providerLogSummary 返回 provider 类型摘要，不包含任何密钥或 params 值。
func providerLogSummary(message wsMessage) []string {
	providers := make([]string, 0, len(message.ProviderConfigs))
	for typ, config := range message.ProviderConfigs {
		if config.Provider == "tencent-asr" || config.Provider == "tencent-tts" {
			providers = append(providers, fmt.Sprintf("%s:%s(appId=%t,secretId=%t,secretKey=%t)",
				string(typ),
				config.Provider,
				providerParamConfigured(config, "appId", "appid"),
				providerSecretConfigured(config, "secretId", "secretID"),
				providerSecretConfigured(config, "secretKey", "secret"),
			))
			continue
		}
		if config.Provider == "tencent-tmt" {
			providers = append(providers, fmt.Sprintf("%s:%s(secretId=%t,secretKey=%t)",
				string(typ),
				config.Provider,
				providerSecretConfigured(config, "secretId", "secretID"),
				providerSecretConfigured(config, "secretKey", "secret"),
			))
			continue
		}
		providers = append(providers, string(typ)+":"+config.Provider)
	}
	sort.Strings(providers)
	return providers
}

func providerParamConfigured(config model.ProviderConfig, keys ...string) bool {
	return providerMapValueConfigured(config.Params, keys...)
}

func providerSecretConfigured(config model.ProviderConfig, keys ...string) bool {
	return providerMapValueConfigured(config.Secrets, keys...)
}

func providerMapValueConfigured(values map[string]string, keys ...string) bool {
	for key, value := range values {
		for _, want := range keys {
			if strings.EqualFold(key, want) && strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

// truncateLogValue 返回适合控制台展示的截断字符串。
func truncateLogValue(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

