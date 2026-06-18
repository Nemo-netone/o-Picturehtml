// Package client 是 SimulSpeak 的 PBX 节点客户端库（SDK），提供节点池管理、负载均衡、WebSocket 连接、
// AI Provider 配置构建以及 PBX 消息中继等能力。api-server 通过本包发现和连接 pbx-node 媒体节点。
//
// 本文件实现 PBX 控制中心 HTTP API 客户端：提供健康检查、通话路由、配置查询等 REST API 调用能力。
// 支持函数式选项模式配置，可自定义 HTTP 客户端、User-Agent 和固定请求头。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

const defaultUserAgent = "simulspeak-sdk/0.1"

// ErrMissingBaseURL 在 baseURL 为空时由 New 返回。
var ErrMissingBaseURL = errors.New("sdk base url is required")

// Client 是 SimulSpeak 控制中心 API 的 HTTP 客户端，封装了健康检查、通话路由、
// 分机配置查询等管理面接口。所有方法均支持并发调用。零值不可用，必须通过 New 或 MustNew 创建。
type Client struct {
	baseURL         *url.URL
	httpClient      *http.Client
	userAgent       string
	headers         http.Header
	providerConfigs map[model.CapabilityType]model.ProviderConfig
}

// Option 是配置 Client 的函数式选项（函数式选项模式）。
type Option func(*Client)

// WithHTTPClient 设置自定义 HTTP 客户端，用于连接池、TLS 配置等场景。
// 传入 nil 时该选项被忽略。
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithUserAgent 覆盖默认的 User-Agent 请求头。空字符串或纯空白字符串会被忽略。
func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if strings.TrimSpace(userAgent) != "" {
			c.userAgent = userAgent
		}
	}
}

// WithHeader 设置每个请求都会携带的固定 HTTP 头。对同一 key 多次调用会覆盖之前的值。
func WithHeader(key, value string) Option {
	return func(c *Client) {
		if key != "" {
			c.headers.Set(key, value)
		}
	}
}

// New 根据给定的控制中心 baseURL 创建 Client。baseURL 必须是绝对 HTTP 地址
// （如 "http://localhost:8080"）。为空返回 ErrMissingBaseURL。
func New(baseURL string, options ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, ErrMissingBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse sdk base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("parse sdk base url: absolute http url is required")
	}

	// 初始化默认值
	client := &Client{
		baseURL:         parsed,
		httpClient:      http.DefaultClient,
		userAgent:       defaultUserAgent,
		headers:         http.Header{},
		providerConfigs: map[model.CapabilityType]model.ProviderConfig{},
	}
	// 应用函数式选项
	for _, option := range options {
		option(client)
	}
	return client, nil
}

// MustNew 与 New 相同，但失败时 panic。适合包级别初始化，此时错误的 baseURL
// 属于程序员失误，应尽早暴露。
func MustNew(baseURL string, options ...Option) *Client {
	client, err := New(baseURL, options...)
	if err != nil {
		panic(err)
	}
	return client
}

// BaseURL 返回客户端当前使用的 API 根地址。
func (c *Client) BaseURL() string {
	if c == nil || c.baseURL == nil {
		return ""
	}
	return c.baseURL.String()
}

// APIError 表示控制中心 API 返回的非 2xx 响应。使用 errors.As 从错误中提取。
type APIError struct {
	StatusCode int    // HTTP 状态码
	Code       int    // 业务错误码
	Message    string // 服务端消息
	Reason     string // 错误原因（Error() 优先展示此字段）
	Body       []byte // 原始响应体
}

// Error 实现 error 接口。优先展示 Reason，其次 Message，最后仅展示状态码。
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return fmt.Sprintf("simulspeak api error: status=%d reason=%s", e.StatusCode, e.Reason)
	}
	if e.Message != "" {
		return fmt.Sprintf("simulspeak api error: status=%d message=%s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("simulspeak api error: status=%d", e.StatusCode)
}

// responseEnvelope 是控制中心 API 的通用 JSON 响应包裹结构。
type responseEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
	Error   string `json:"error"`
}

// ---------------------------------------------------------------------------
// 响应类型 —— 各类 API 的请求/响应数据结构
// ---------------------------------------------------------------------------

// HealthResult 是 /health 端点的响应，表示服务健康状态。
type HealthResult struct {
	Status  string    `json:"status"`
	Service string    `json:"service"`
	Time    time.Time `json:"time"`
}

// RouteStrategy 是通话路由的节点选择算法类型。
type RouteStrategy string

const (
	// RouteStrategyRoundRobin 轮询策略：依次分配到各可用节点。
	RouteStrategyRoundRobin RouteStrategy = "round_robin"
	// RouteStrategyWeightedRoundRobin 加权轮询：按节点权重比例分配。
	RouteStrategyWeightedRoundRobin RouteStrategy = "weighted_round_robin"
	// RouteStrategyLeastConnections 最小连接数：优先分配给连接数最少的节点。
	RouteStrategyLeastConnections RouteStrategy = "least_connections"
	// RouteStrategyZoneAffinity 同区亲和：优先将通话路由到同一区域的节点。
	RouteStrategyZoneAffinity RouteStrategy = "zone_affinity"
	// RouteStrategyTenantAffinity 租户亲和：同一租户的通话尽量路由到同一节点。
	RouteStrategyTenantAffinity RouteStrategy = "tenant_affinity"
)

// RouteType 分类通话方向：内部、呼入或呼出。
type RouteType string

const (
	// RouteTypeInternal 内部分机互拨。
	RouteTypeInternal RouteType = "internal"
	// RouteTypeOutbound 外呼。
	RouteTypeOutbound RouteType = "outbound"
	// RouteTypeInbound 呼入。
	RouteTypeInbound RouteType = "inbound"
)

// RouteRequest 是 POST /api/v1/calls/route 的请求体，承载路由决策所需的所有参数。
type RouteRequest struct {
	TenantID string        `json:"tenantId"` // 租户 ID
	Caller   string        `json:"caller"`   // 主叫号码
	Callee   string        `json:"callee"`   // 被叫号码
	Media    string        `json:"media"`    // 媒体类型（webrtc / sip）
	NeedAI   bool          `json:"needAI"`   // 是否需要 AI 管道
	Strategy RouteStrategy `json:"strategy"` // 路由策略
	Zone     string        `json:"zone"`     // 偏好区域
	Language string        `json:"language"` // 对话语言
	ASRModel string        `json:"asrModel"` // ASR 模型名称
}

// RouteResult 是 RouteCall 的返回值。当请求 AI 且可用时，AIPipeline 包含选定的 VAD/ASR/TTS 音频能力 ID。
type RouteResult struct {
	CallID      string            `json:"callId"`               // 通话唯一标识
	RouteType   RouteType         `json:"routeType"`            // 路由类型
	GatewayNode string            `json:"gatewayNode"`          // 信令网关节点
	MediaNode   string            `json:"mediaNode"`            // 媒体节点
	TurnNode    string            `json:"turnNode"`             // TURN 节点
	AIPipeline  *model.AIPipeline `json:"aiPipeline,omitempty"` // AI 管道（可选）
}

// CallEndResult 是 EndCall 的响应，返回已结束的通话 ID。
type CallEndResult struct {
	CallID string `json:"callId"`
}

// ---------------------------------------------------------------------------
// API 方法 —— 控制中心 REST API 调用
// ---------------------------------------------------------------------------

// Health 调用 GET /api/v1/health 返回控制中心健康状态。
func (c *Client) Health(ctx context.Context) (HealthResult, error) {
	var result HealthResult
	err := c.do(ctx, http.MethodGet, "/api/v1/health", nil, nil, &result)
	return result, err
}

// RouteCall 请求控制中心为新通话选择媒体节点/网关/TURN 节点，并在 NeedAI 为 true 时
// 组装 PBX 音频管道（VAD→ASR→TTS）。这是同传会话建立时最关键的路由决策调用。
func (c *Client) RouteCall(ctx context.Context, request RouteRequest) (RouteResult, error) {
	var result RouteResult
	err := c.do(ctx, http.MethodPost, "/api/v1/calls/route", nil, request, &result)
	return result, err
}

// GetCall 根据 callID 查询通话会话详情。
func (c *Client) GetCall(ctx context.Context, callID string) (model.CallSession, error) {
	var result model.CallSession
	err := c.do(ctx, http.MethodGet, "/api/v1/calls/"+url.PathEscape(callID), nil, nil, &result)
	return result, err
}

// EndCall 结束指定 callID 的通话。
func (c *Client) EndCall(ctx context.Context, callID string) (CallEndResult, error) {
	var result CallEndResult
	err := c.do(ctx, http.MethodDelete, "/api/v1/calls/"+url.PathEscape(callID), nil, nil, &result)
	return result, err
}

// GetExtension 查询指定租户下的分机配置。
func (c *Client) GetExtension(ctx context.Context, tenantID, extension string) (model.Extension, error) {
	var result model.Extension
	err := c.do(ctx, http.MethodGet, extensionPath(tenantID, extension), nil, nil, &result)
	return result, err
}

// GetPresence 查询指定分机的当前在线状态。
func (c *Client) GetPresence(ctx context.Context, tenantID, extension string) (model.Presence, error) {
	var result model.Presence
	err := c.do(ctx, http.MethodGet, "/api/v1/extensions/"+url.PathEscape(tenantID)+"/"+url.PathEscape(extension)+"/presence", nil, nil, &result)
	return result, err
}

// do 执行 HTTP 请求，解析 JSON 响应包裹结构，将 data 字段反序列化到 out。
// 非 2xx 响应统一映射为 *APIError。
//
// 处理流程：
// 1. 序列化请求体 → 2. 构造 HTTP 请求（设置 Accept/UA/自定义头）
// 3. 执行请求 → 4. 读取响应体 → 5. 解析 JSON envelope → 6. 提取 data 字段
func (c *Client) do(ctx context.Context, method, requestPath string, query url.Values, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(requestPath, query), reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	// 设置标准请求头
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	// 非 2xx 响应直接返回 APIError
	if resp.StatusCode >= http.StatusBadRequest {
		return apiError(resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}

	// 解析 JSON 响应 envelope，提取 data 字段
	var envelope responseEnvelope[json.RawMessage]
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode response envelope: %w", err)
	}
	if envelope.Error != "" {
		return &APIError{StatusCode: resp.StatusCode, Code: envelope.Code, Message: envelope.Message, Reason: envelope.Error, Body: data}
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

// endpoint 基于 baseURL、路径和查询参数拼接完整请求 URL。
func (c *Client) endpoint(requestPath string, query url.Values) string {
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/" + strings.TrimLeft(requestPath, "/")
	u.RawQuery = query.Encode()
	return u.String()
}

// apiError 将非 2xx HTTP 响应转换为 *APIError。优先解析 JSON envelope，
// 解析失败则退化为仅包含状态码和响应体的错误。
func apiError(statusCode int, body []byte) error {
	var envelope responseEnvelope[json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err == nil {
		return &APIError{
			StatusCode: statusCode,
			Code:       envelope.Code,
			Message:    envelope.Message,
			Reason:     envelope.Error,
			Body:       body,
		}
	}
	return &APIError{StatusCode: statusCode, Body: body}
}

// extensionPath 生成分机配置 API 路径：/api/v1/config/extensions/{tenantID}/{extension}
func extensionPath(tenantID, extension string) string {
	return "/api/v1/config/extensions/" + url.PathEscape(tenantID) + "/" + url.PathEscape(extension)
}
