// PBX HTTP/WS API：对外暴露的 pbx-node 控制面接口
//
// 本文件是 pbx-node 的 HTTP 服务入口，负责：
//   - 组装 WebRTC 管理器 + 事件总线 + Provider 配置
//   - 注册 /pbx/health（健康检查）和 /pbx/ws（WebSocket 控制通道）路由
//   - 处理 media 节点的 HTTP 请求分发
//
// 每个 pbx-node 启动时会创建一个本实例，绑定到配置的控制地址。
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/control"
	"github.com/SATA260/SimulSpeak1/internal/pbx/webrtc"
)

// Dependencies 是 PBX HTTP API 的依赖注入结构。
type Dependencies struct {
	WebRTC          *webrtc.Manager                         // WebRTC 管理器（音频管线核心）
	ProviderConfigs map[model.CapabilityType]model.ProviderConfig // AI Provider 配置
}

// API 是 pbx-node HTTP 服务的主结构：持有控制服务器和 chi 路由。
type API struct {
	control    *control.Server
	httpRouter http.Handler
}

// HealthResult 健康检查响应体。
type HealthResult struct {
	Status  string    `json:"status"`
	Service string    `json:"service"`
	Time    time.Time `json:"time"`
}

// New 创建 pbx-node 独享的 HTTP/WebSocket 路由入口。
// 内部创建事件总线和控制服务器，将 WebRTC 管理器注入控制通道。
func New(deps Dependencies) *API {
	// 创建内存事件总线，用于 PBX 内部事件分发
	bus := eventbus.NewMemoryBus()
	if deps.WebRTC != nil {
		deps.WebRTC.SetEventBus(bus)
	}
	api := &API{
		control: &control.Server{
			WebRTC:          deps.WebRTC,
			ProviderConfigs: model.CloneProviderConfigs(deps.ProviderConfigs),
			EventBus:        bus,
		},
	}
	api.httpRouter = api.routes()
	return api
}

// ServeHTTP 实现 http.Handler 接口，将所有 HTTP 请求转发给 chi 路由器。
func (api *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.httpRouter.ServeHTTP(w, r)
}

// routes 注册所有 HTTP 路由：健康检查 + WebSocket 控制通道。
func (api *API) routes() http.Handler {
	r := chi.NewRouter()
	// 404 / 405 统一 JSON 响应，便于客户端错误处理
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})

	// /pbx/health：健康检查端点，返回节点状态
	r.Get("/pbx/health", api.handleHealth)
	// /pbx/ws：WebSocket 控制通道，api-server 通过此端点与 pbx-node 通信
	r.Get("/pbx/ws", api.control.ServeHTTP)
	return r
}

// handleHealth 处理 /pbx/health 请求：返回 pbx-node 的运行状态。
func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResult{
		Status:  "ok",
		Service: "pbx-node",
		Time:    time.Now().UTC(),
	})
}

// writeJSON 将 value 序列化为 JSON 并写入 HTTP 响应。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
