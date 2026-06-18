// HTTP API入口：路由注册+依赖注入+中间件装配
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/gateway"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

type PBXControl interface {
	Send(context.Context, pbxprotocol.Message) error
}

type PBXControlBinder interface {
	PBXControl
	Bind(ctx context.Context, connectionID, callID, key string) (string, error)
	Unbind(connectionID string)
}

type MediaPicker interface {
	Pick(policy, key string) (*model.Node, error)
}

type MediaNodeSource interface {
	Nodes() []*model.Node
}

type Dependencies struct {
	Registry     *registry.Registry
	Router       *router.Router
	Config       *configcenter.Store
	Sessions     *session.Manager
	Gateway      *gateway.Gateway
	PBXControl   PBXControl
	MediaPicker  MediaPicker
	SessionStore *sessionstore.Store
	LLM          *llm.Client
}

type API struct {
	Dependencies
	httpRouter   http.Handler
	mu           sync.RWMutex
	frontConns   map[string]*wsConn
	interpreters map[string]*interpreterSession
}

// New 创建 HTTP API 处理器。
func New(deps Dependencies) *API {
	if deps.Gateway == nil {
		deps.Gateway = gateway.New(gateway.Options{})
	}
	api := &API{
		Dependencies: deps,
		frontConns:   map[string]*wsConn{},
		interpreters: map[string]*interpreterSession{},
	}
	api.httpRouter = api.routes()
	return api
}

// SetPBXControl 注入 PBX 控制通道实例（由 api-server main 在启动时调用）。
// 由于 PBX 控制连接池在 API 实例创建之后才启动，因此需要通过此方法延迟注入。
func (api *API) SetPBXControl(control PBXControl) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.PBXControl = control
}

func (api *API) pbxControl() PBXControl {
	api.mu.RLock()
	defer api.mu.RUnlock()
	return api.PBXControl
}

func (api *API) bindPBXControl(ctx context.Context, connectionID, callID string) (string, error) {
	control := api.pbxControl()
	if control == nil {
		return "", errors.New("pbx control is not configured")
	}
	binder, ok := control.(PBXControlBinder)
	if !ok {
		return "", nil
	}
	return binder.Bind(ctx, connectionID, callID, firstBridgeValue(callID, connectionID))
}

func (api *API) unbindPBXControl(connectionID string) {
	control := api.pbxControl()
	binder, ok := control.(PBXControlBinder)
	if ok {
		binder.Unbind(connectionID)
	}
}

// ServeHTTP 执行 HTTP 路由分发，并在外层统一记录请求日志。
func (api *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	recorder := &requestLogResponseWriter{ResponseWriter: w}
	defer logHTTPRequest(r, recorder, started)
	api.httpRouter.ServeHTTP(recorder, r)
}

// routes 注册 api-server HTTP/WebSocket 路由。
func (api *API) routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(api.handleNotFound)
	r.MethodNotAllowed(api.handleMethodNotAllowed)

	r.Get("/ws", api.handleWebSocket)
	r.Route("/api/v1", api.registerV1Routes)
	return r
}

func (api *API) registerV1Routes(r chi.Router) {
	r.Get("/health", api.handleHealth)
	r.Get("/system/nodes", api.handleSystemNodes)
	r.Get("/media/nodes", api.handleMediaNodes)
	r.Get("/media/pick", api.handleMediaPick)
	r.Get("/interpreter/sessions", api.handleListInterpreterSessions)
	r.Get("/interpreter/sessions/{callID}", api.handleGetInterpreterSessionDetail)
	r.Post("/interpreter/sessions/{callID}/vocabulary-tasks", api.handleCreateVocabularyTask)
	r.Get("/interpreter/sessions/{callID}/vocabulary-tasks", api.handleListVocabularyTasks)
	r.Get("/vocabulary-tasks/{taskID}", api.handleGetVocabularyTask)
	r.Post("/calls/route", api.handleCallRoute)
	r.Get("/calls/{callID}", api.handleGetCall)
	r.Delete("/calls/{callID}", api.handleEndCall)
	r.Get("/config/extensions/{tenantID}/{extension}", api.handleGetExtension)
	r.Get("/extensions/{tenantID}/{extension}/presence", api.handleGetPresence)
}
