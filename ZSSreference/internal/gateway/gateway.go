//  网关：信令网关抽象
package gateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

var (
	ErrInvalidToken       = errors.New("invalid token")
	ErrTooManyConnections = errors.New("too many connections")
	ErrConnectionNotFound = errors.New("connection not found")
	ErrPresenceNotFound   = errors.New("presence not found")
)

type MessageType string

const (
	MessageOffer  MessageType = "offer"
	MessageAnswer MessageType = "answer"
	MessageICE    MessageType = "ice"
)

type Options struct {
	MaxConnections   int
	HeartbeatTimeout time.Duration
}

type Connection struct {
	ID        string
	TenantID  string
	Extension string
	CreatedAt time.Time
	LastSeen  time.Time
}

type Message struct {
	Type      MessageType
	Payload   string
	Timestamp time.Time
}

type RecoverResult struct {
	ConnectionID string
	CallID       string
}

type Gateway struct {
	mu          sync.Mutex
	options     Options
	connections map[string]Connection
	presence    map[string]model.Presence
	messages    map[string][]Message
	callByConn  map[string]string
	recovery    map[string]RecoverResult
	providers   map[string]map[model.CapabilityType]model.ProviderConfig
}

// New 创建信令网关，默认最多 10000 连接、心跳超时 45s。
func New(options Options) *Gateway {
	if options.MaxConnections <= 0 {
		options.MaxConnections = 10000
	}
	if options.HeartbeatTimeout <= 0 {
		options.HeartbeatTimeout = 45 * time.Second
	}
	return &Gateway{
		options:     options,
		connections: map[string]Connection{},
		presence:    map[string]model.Presence{},
		messages:    map[string][]Message{},
		callByConn:  map[string]string{},
		recovery:    map[string]RecoverResult{},
		providers:   map[string]map[model.CapabilityType]model.ProviderConfig{},
	}
}

// Connect 解析 "tenantID:extension" 格式的 token，创建 WebSocket 连接并设置 presence 为 online。
func (g *Gateway) Connect(ctx context.Context, token string) (Connection, error) {
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	tenantID, extension, err := parseToken(token)
	if err != nil {
		return Connection{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.connections) >= g.options.MaxConnections {
		return Connection{}, ErrTooManyConnections
	}

	now := time.Now().UTC()
	conn := Connection{
		ID:        idgen.SessionID(),
		TenantID:  tenantID,
		Extension: extension,
		CreatedAt: now,
		LastSeen:  now,
	}
	g.connections[conn.ID] = conn
	g.presence[presenceKey(tenantID, extension)] = model.Presence{
		TenantID:     tenantID,
		Extension:    extension,
		GatewayID:    "signaling-gateway",
		Status:       model.PresenceStatusOnline,
		ConnectionID: conn.ID,
		UpdatedAt:    now,
	}
	return conn, nil
}

// Disconnect 断开连接并将 presence 设为 offline；若连接在通话中则保存 recovery 信息供重连恢复。
func (g *Gateway) Disconnect(ctx context.Context, connectionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	conn, ok := g.connections[connectionID]
	if !ok {
		return ErrConnectionNotFound
	}
	delete(g.connections, connectionID)
	delete(g.providers, connectionID)
	g.presence[presenceKey(conn.TenantID, conn.Extension)] = model.Presence{
		TenantID:     conn.TenantID,
		Extension:    conn.Extension,
		GatewayID:    "signaling-gateway",
		Status:       model.PresenceStatusOffline,
		ConnectionID: connectionID,
		UpdatedAt:    time.Now().UTC(),
	}
	if callID := g.callByConn[connectionID]; callID != "" {
		g.recovery[connectionID] = RecoverResult{ConnectionID: connectionID, CallID: callID}
	}
	return nil
}

// SetProviderConfigs 为指定 WebSocket 连接保存客户端上报的 ASR/TTS provider 参数。
func (g *Gateway) SetProviderConfigs(connectionID string, configs map[model.CapabilityType]model.ProviderConfig) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.connections[connectionID]; !ok {
		return ErrConnectionNotFound
	}
	g.providers[connectionID] = cloneProviderConfigs(configs)
	return nil
}

// ProviderConfigs 返回指定连接保存的 provider 参数快照。
func (g *Gateway) ProviderConfigs(connectionID string) map[model.CapabilityType]model.ProviderConfig {
	g.mu.Lock()
	defer g.mu.Unlock()
	return cloneProviderConfigs(g.providers[connectionID])
}

// GetConnection 按连接 ID 查询连接信息。
func (g *Gateway) GetConnection(connectionID string) (Connection, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	conn, ok := g.connections[connectionID]
	if !ok {
		return Connection{}, ErrConnectionNotFound
	}
	return conn, nil
}

// GetPresence 查询某分机的在线状态。
func (g *Gateway) GetPresence(ctx context.Context, tenantID, extension string) (*model.Presence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	presence, ok := g.presence[presenceKey(tenantID, extension)]
	if !ok {
		return nil, ErrPresenceNotFound
	}
	return &presence, nil
}

// SendOffer 向连接发送 SDP offer 消息。
func (g *Gateway) SendOffer(ctx context.Context, connectionID, sdp string) error {
	return g.appendMessage(ctx, connectionID, MessageOffer, sdp)
}

// SendAnswer 向连接发送 SDP answer 消息。
func (g *Gateway) SendAnswer(ctx context.Context, connectionID, sdp string) error {
	return g.appendMessage(ctx, connectionID, MessageAnswer, sdp)
}

// SendICE 向连接发送 ICE candidate 消息。
func (g *Gateway) SendICE(ctx context.Context, connectionID, candidate string) error {
	return g.appendMessage(ctx, connectionID, MessageICE, candidate)
}

// Messages 返回该连接的消息队列快照。
func (g *Gateway) Messages(connectionID string) []Message {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Message(nil), g.messages[connectionID]...)
}

// Touch 刷新连接的 LastSeen 时间（心跳）。
func (g *Gateway) Touch(connectionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	conn, ok := g.connections[connectionID]
	if !ok {
		return ErrConnectionNotFound
	}
	conn.LastSeen = time.Now().UTC()
	g.connections[connectionID] = conn
	return nil
}

// CloseIdle 断开所有心跳超时的空闲连接。
func (g *Gateway) CloseIdle(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	toClose := make([]string, 0)
	g.mu.Lock()
	for id, conn := range g.connections {
		if now.Sub(conn.LastSeen) > g.options.HeartbeatTimeout {
			toClose = append(toClose, id)
		}
	}
	g.mu.Unlock()
	for _, id := range toClose {
		_ = g.Disconnect(ctx, id)
	}
	return nil
}

// BindCall 将连接与通话 ID 绑定（用于断开时记录 recovery 信息）。
func (g *Gateway) BindCall(connectionID, callID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.connections[connectionID]; !ok {
		return ErrConnectionNotFound
	}
	g.callByConn[connectionID] = callID
	return nil
}

// RecoverSession 查询断开前绑定的通话 ID（用于断线重连恢复）。
func (g *Gateway) RecoverSession(ctx context.Context, connectionID string) (RecoverResult, error) {
	if err := ctx.Err(); err != nil {
		return RecoverResult{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	result, ok := g.recovery[connectionID]
	if !ok {
		return RecoverResult{}, ErrConnectionNotFound
	}
	return result, nil
}

// appendMessage 向连接的待发送队列追加一条信令消息。
func (g *Gateway) appendMessage(ctx context.Context, connectionID string, typ MessageType, payload string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.connections[connectionID]; !ok {
		return ErrConnectionNotFound
	}
	g.messages[connectionID] = append(g.messages[connectionID], Message{
		Type:      typ,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

// parseToken 解析 "tenantID:extension" 格式的认证 token。
func parseToken(token string) (string, string, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidToken
	}
	return parts[0], parts[1], nil
}

// presenceKey 构建 presence 存储键：tenantID:extension。
func presenceKey(tenantID, extension string) string {
	return tenantID + ":" + extension
}

// cloneProviderConfigs 深拷贝 provider 配置，避免调用方修改网关内部状态。
func cloneProviderConfigs(configs map[model.CapabilityType]model.ProviderConfig) map[model.CapabilityType]model.ProviderConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make(map[model.CapabilityType]model.ProviderConfig, len(configs))
	for typ, config := range configs {
		out[typ] = model.ProviderConfig{
			Provider: config.Provider,
			Endpoint: config.Endpoint,
			Params:   cloneStringMap(config.Params),
			Secrets:  cloneStringMap(config.Secrets),
		}
	}
	return out
}

// cloneStringMap 深拷贝字符串 map。
func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

