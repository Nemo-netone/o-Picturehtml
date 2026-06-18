//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var ErrWebSocketClosed = errors.New("sdk websocket is closed")

const (
	WSResponseModeDebug   = "debug"
	WSResponseModeCompact = "compact"
)

type WSConnectOptions struct {
	Token           string
	TenantID        string
	ClientID        string
	ResponseMode    string
	ProviderConfigs map[model.CapabilityType]model.ProviderConfig
}

type WSMessage struct {
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

type WebSocket struct {
	conn         net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	writeMu      sync.Mutex
	connectionID string
	helloAck     WSMessage
}

// ConnectWebSocket 建立到 PBX 服务器的 WebSocket 控制通道，并发送 client_hello，返回服务端脱敏后的确认消息。
func (c *Client) ConnectWebSocket(ctx context.Context, options WSConnectOptions) (*WebSocket, WSMessage, error) {
	if options.TenantID == "" {
		options.TenantID = "tenant-a"
	}
	if options.ClientID == "" {
		options.ClientID = "sdk-client"
	}
	options.ResponseMode = normalizeWSResponseMode(options.ResponseMode)
	configs := cloneProviderConfigMap(c.providerConfigs)
	if configs == nil {
		configs = map[model.CapabilityType]model.ProviderConfig{}
	}
	for typ, config := range options.ProviderConfigs {
		configs[typ] = cloneProviderConfig(config)
	}

	ws, err := c.dialWebSocket(ctx, options)
	if err != nil {
		return nil, WSMessage{}, err
	}
	connected, err := ws.Read(ctx)
	if err != nil {
		_ = ws.Close()
		return nil, WSMessage{}, err
	}
	if connected.Type != "connected" {
		_ = ws.Close()
		return nil, WSMessage{}, fmt.Errorf("unexpected websocket greeting: %s", connected.Type)
	}
	if err := ws.Send(ctx, WSMessage{
		Type:            "client_hello",
		RequestID:       requestID("hello"),
		ConnectionID:    connected.ConnectionID,
		TenantID:        options.TenantID,
		Extension:       options.ClientID,
		ClientID:        options.ClientID,
		ResponseMode:    options.ResponseMode,
		ProviderConfigs: configs,
	}); err != nil {
		_ = ws.Close()
		return nil, WSMessage{}, err
	}
	ack, err := ws.Read(ctx)
	if err != nil {
		_ = ws.Close()
		return nil, WSMessage{}, err
	}
	if ack.Type != "client_hello_ack" {
		_ = ws.Close()
		return nil, WSMessage{}, fmt.Errorf("unexpected client hello ack: %s", ack.Type)
	}
	ws.connectionID = connected.ConnectionID
	ws.helloAck = ack
	return ws, ack, nil
}

// normalizeWSResponseMode 归一化 WebSocket 回包模式，默认保留调试模式以兼容旧客户端。
func normalizeWSResponseMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), WSResponseModeCompact) {
		return WSResponseModeCompact
	}
	return WSResponseModeDebug
}

// ConnectionID 返回 PBX 为该 WebSocket 控制连接分配的 ID。
func (ws *WebSocket) ConnectionID() string {
	if ws == nil {
		return ""
	}
	return ws.connectionID
}

// HelloAck 返回 client_hello 的确认消息，provider secrets 已由 PBX 脱敏。
func (ws *WebSocket) HelloAck() WSMessage {
	if ws == nil {
		return WSMessage{}
	}
	return ws.helloAck
}

// SendWebRTCOffer 发送用户 WebRTC offer 给 PBX。
func (ws *WebSocket) SendWebRTCOffer(ctx context.Context, callID, userID, sdp string) error {
	return ws.Send(ctx, WSMessage{
		Type:      "webrtc_offer",
		RequestID: requestID("offer"),
		CallID:    callID,
		UserID:    userID,
		SDP:       sdp,
		Metadata:  map[string]string{"media": "audio", "source": "simulspeak-sdk"},
	})
}

// SendICE 发送用户侧 ICE candidate 给 PBX。
func (ws *WebSocket) SendICE(ctx context.Context, callID, userID, candidate string) error {
	return ws.Send(ctx, WSMessage{
		Type:      "ice",
		RequestID: requestID("ice"),
		CallID:    callID,
		UserID:    userID,
		Candidate: candidate,
	})
}

// SendTTSCommand 请求 PBX 调用 TTS 并通过 WebRTC 播放给用户。
func (ws *WebSocket) SendTTSCommand(ctx context.Context, callID, userID, text, voice, language string) error {
	return ws.Send(ctx, WSMessage{
		Type:      "tts_command",
		RequestID: requestID("tts"),
		CallID:    callID,
		UserID:    userID,
		Text:      text,
		Voice:     voice,
		Language:  language,
	})
}

// Send 发送一条 JSON WebSocket 文本帧。
func (ws *WebSocket) Send(ctx context.Context, message WSMessage) error {
	if ws == nil || ws.conn == nil {
		return ErrWebSocketClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	if ws.conn == nil {
		return ErrWebSocketClosed
	}
	_ = ws.conn.SetWriteDeadline(deadlineFromContext(ctx, 10*time.Second))
	return writeWSFrame(ws.writer, 0x1, data, true)
}

// Read 读取一条 JSON WebSocket 文本帧。
func (ws *WebSocket) Read(ctx context.Context) (WSMessage, error) {
	if ws == nil || ws.conn == nil {
		return WSMessage{}, ErrWebSocketClosed
	}
	if err := ctx.Err(); err != nil {
		return WSMessage{}, err
	}
	_ = ws.conn.SetReadDeadline(deadlineFromContext(ctx, 30*time.Second))
	for {
		opcode, payload, err := readWSFrame(ws.reader, false)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return WSMessage{}, ctxErr
			}
			return WSMessage{}, err
		}
		switch opcode {
		case 0x1:
			var message WSMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				return WSMessage{}, err
			}
			return message, nil
		case 0x8:
			return WSMessage{}, ErrWebSocketClosed
		case 0x9:
			if err := writeWSFrame(ws.writer, 0xA, payload, true); err != nil {
				return WSMessage{}, err
			}
		}
	}
}

// Close 关闭 WebSocket 连接。
func (ws *WebSocket) Close() error {
	if ws == nil || ws.conn == nil {
		return nil
	}
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	_ = writeWSFrame(ws.writer, 0x8, nil, true)
	err := ws.conn.Close()
	ws.conn = nil
	return err
}

// deadlineFromContext 返回本次网络读写的 deadline；若 context 更早超时则优先使用 context deadline。
func deadlineFromContext(ctx context.Context, fallback time.Duration) time.Time {
	deadline := time.Now().Add(fallback)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

// WebSocketURL 返回 PBX WebSocket 地址。
func (c *Client) WebSocketURL(options WSConnectOptions) string {
	u := *c.baseURL
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/ws"
	query := url.Values{}
	token := options.Token
	if token == "" {
		token = options.TenantID + ":" + options.ClientID
	}
	query.Set("token", token)
	u.RawQuery = query.Encode()
	return u.String()
}

// dialWebSocket 完成 SDK 到 PBX 的 RFC6455 握手。
func (c *Client) dialWebSocket(ctx context.Context, options WSConnectOptions) (*WebSocket, error) {
	wsURL := c.WebSocketURL(options)
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	switch parsed.Scheme {
	case "ws":
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", parsed.Host)
	case "wss":
		conn, err = (&tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{ServerName: parsed.Hostname()}}).DialContext(ctx, "tcp", parsed.Host)
	default:
		return nil, errors.New("websocket url must use ws or wss")
	}
	if err != nil {
		return nil, err
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		_ = conn.Close()
		return nil, err
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)
	path := parsed.RequestURI()
	requestScheme := "http"
	if parsed.Scheme == "wss" {
		requestScheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestScheme+"://"+parsed.Host+path, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", encodedKey)
	req.Header.Set("User-Agent", c.userAgent)
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if err := req.Write(writer); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := writer.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("websocket upgrade failed: %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != websocketAccept(encodedKey) {
		_ = conn.Close()
		return nil, errors.New("websocket accept mismatch")
	}
	return &WebSocket{conn: conn, reader: reader, writer: writer}, nil
}

// websocketAccept 根据客户端 key 计算服务端应答。
func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// requestID 生成 WebSocket 请求 ID。
func requestID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// readWSFrame 读取 WebSocket 帧。
func readWSFrame(reader *bufio.Reader, requireMask bool) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	if requireMask && !masked {
		return 0, nil, errors.New("websocket frame must be masked")
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(reader, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

// writeWSFrame 写入 WebSocket 帧。
func writeWSFrame(writer *bufio.Writer, opcode byte, payload []byte, masked bool) error {
	first := byte(0x80) | opcode
	header := []byte{first, 0}
	length := len(payload)
	switch {
	case length < 126:
		header[1] = byte(length)
	case length <= 0xFFFF:
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(length))
	default:
		header[1] = 127
		header = binary.BigEndian.AppendUint64(header, uint64(length))
	}
	if masked {
		header[1] |= 0x80
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		if _, err := writer.Write(maskKey[:]); err != nil {
			return err
		}
		payload = append([]byte(nil), payload...)
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

