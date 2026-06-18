// HTTP API层：路由注册+WebSocket处理+同传解释器+PBX消息桥接+请求日志
package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/api-server/httpapi"
	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
)

// 作用: 验证 Test H T T P_ Health 场景的行为。
func TestHTTP_Health(t *testing.T) {
	api := newTestAPI()

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health failed %d: %s", rec.Code, rec.Body.String())
	}
}

// 作用: 验证 Test H T T P_ Request Logging 场景的行为。
func TestHTTP_RequestLogging(t *testing.T) {
	api := newTestAPI()
	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
	})

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	logLine := buf.String()
	if !strings.Contains(logLine, "HTTP 请求") || !strings.Contains(logLine, `"/api/v1/health"`) || !strings.Contains(logLine, `"status":200`) {
		t.Fatalf("expected request access log, got %s", logLine)
	}
}

// 作用: 验证节点、能力和配置写入接口不再对外暴露。
func TestHTTP_RemovedManagementRoutes_NotFound(t *testing.T) {
	api := newTestAPI()

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodPost, path: "/api/v1/nodes/register", body: model.Node{}},
		{method: http.MethodGet, path: "/api/v1/nodes"},
		{method: http.MethodDelete, path: "/api/v1/nodes/media/media-01"},
		{method: http.MethodPost, path: "/api/v1/capabilities/register", body: model.Capability{}},
		{method: http.MethodGet, path: "/api/v1/capabilities"},
		{method: http.MethodPut, path: "/api/v1/config/extensions/tenant-a/1001", body: model.Extension{}},
		{method: http.MethodGet, path: "/api/v1/config/watch?tenantId=tenant-a&resource=extensions"},
	}

	for _, route := range routes {
		var rec *httptest.ResponseRecorder
		if route.body != nil {
			rec = doJSON(api, route.method, route.path, route.body)
		} else {
			rec = httptest.NewRecorder()
			api.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		}
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s expected 404/405, got %d: %s", route.method, route.path, rec.Code, rec.Body.String())
		}
	}
}

// 作用: 验证 Test H T T P_ Call Route_ Success 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestHTTP_CallRoute_Success(t *testing.T) {
	api := newTestAPI()
	_ = api.Registry.Register(context.Background(), &model.Node{ID: "media-01", Type: model.NodeTypeMedia, Endpoint: "127.0.0.1:8021", Status: model.NodeStatusUp, MaxCalls: 100})

	rec := doJSON(api, http.MethodPost, "/api/v1/calls/route", router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "media-01") {
		t.Fatalf("expected media node in response: %s", rec.Body.String())
	}
}

// 作用: 验证 Test H T T P_ Call Route_ No Available Node 场景的行为。
func TestHTTP_CallRoute_NoAvailableNode(t *testing.T) {
	api := newTestAPI()

	rec := doJSON(api, http.MethodPost, "/api/v1/calls/route", router.RouteRequest{TenantID: "tenant-a", Caller: "1001", Callee: "1002"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// 作用: 验证 PBX 自动注册到 registry 后，HTTP 通话路由仍能组装 AI pipeline。
func TestHTTP_CallRouteWithRegisteredAICapabilities(t *testing.T) {
	api := newTestAPI()
	_ = api.Registry.Register(context.Background(), &model.Node{ID: "media-01", Type: model.NodeTypeMedia, Endpoint: "127.0.0.1:8021", Status: model.NodeStatusUp, MaxCalls: 100})

	for _, capability := range []model.Capability{
		{ID: "vad-main", Type: model.CapabilityTypeVAD, Protocol: "grpc", MaxConcurrency: 10},
		{ID: "asr-zh", Type: model.CapabilityTypeASR, Protocol: "grpc", Languages: []string{"zh-CN"}, Models: []string{"general"}, MaxConcurrency: 10},
		{ID: "tts-zh", Type: model.CapabilityTypeTTS, Protocol: "grpc", Languages: []string{"zh-CN"}, MaxConcurrency: 10},
	} {
		if err := api.Registry.RegisterCapability(context.Background(), capability); err != nil {
			t.Fatalf("register capability: %v", err)
		}
	}

	rec := doJSON(api, http.MethodPost, "/api/v1/calls/route", router.RouteRequest{
		TenantID: "tenant-a",
		Caller:   "1001",
		Callee:   "1002",
		NeedAI:   true,
		Language: "zh-CN",
		ASRModel: "general",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"aiPipeline"`) {
		t.Fatalf("expected ai route, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_MediaPick(t *testing.T) {
	api := newTestAPI()
	_ = api.Registry.Register(context.Background(), &model.Node{ID: "media-a", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8081/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 100, CurrentCalls: 5})
	_ = api.Registry.Register(context.Background(), &model.Node{ID: "media-b", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8082/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 100, CurrentCalls: 1})
	pool := sdk.NewNodePool(api.Registry, model.NodeTypeMedia)
	if err := pool.Start(context.Background()); err != nil {
		t.Fatalf("start media pool: %v", err)
	}
	api.Dependencies.MediaPicker = pool

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/media/pick?policy=least_load&key=call-1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "media-b") {
		t.Fatalf("expected least loaded media node, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_MediaNodesObservable(t *testing.T) {
	api := newTestAPI()
	for _, node := range []*model.Node{
		{ID: "media-a", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8081/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 100, CurrentCalls: 10},
		{ID: "media-b", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8082/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 20, CurrentCalls: 20},
		{ID: "media-c", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8083/pbx/ws", Status: model.NodeStatusDraining, MaxCalls: 50, CurrentCalls: 1},
	} {
		if err := api.Registry.Register(context.Background(), node); err != nil {
			t.Fatalf("register node: %v", err)
		}
	}
	pool := sdk.NewNodePool(api.Registry, model.NodeTypeMedia)
	if err := pool.Start(context.Background()); err != nil {
		t.Fatalf("start media pool: %v", err)
	}
	api.Dependencies.MediaPicker = pool

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/media/nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Code int                      `json:"code"`
		Data httpapi.MediaNodesResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.Summary.Total != 3 || response.Data.Summary.Available != 1 || response.Data.Summary.Unavailable != 2 {
		t.Fatalf("unexpected summary: %#v", response.Data.Summary)
	}
	if response.Data.Summary.Up != 2 || response.Data.Summary.Draining != 1 || response.Data.Summary.Capacity != 170 || response.Data.Summary.CurrentCalls != 31 {
		t.Fatalf("unexpected load summary: %#v", response.Data.Summary)
	}
	if len(response.Data.Nodes) != 3 || response.Data.Nodes[0].ID != "media-a" || response.Data.Nodes[2].ID != "media-c" {
		t.Fatalf("unexpected nodes: %#v", response.Data.Nodes)
	}
}

func TestHTTP_SystemNodesObservable(t *testing.T) {
	api := newTestAPI()
	for _, node := range []*model.Node{
		{ID: "media-a", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8081/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 100, CurrentCalls: 10},
		{ID: "media-b", Type: model.NodeTypeMedia, Endpoint: "ws://127.0.0.1:8082/pbx/ws", Status: model.NodeStatusDraining, MaxCalls: 20, CurrentCalls: 20},
		{ID: "worker-a", Type: model.NodeTypeWorker, Endpoint: "worker://worker-a", Status: model.NodeStatusUp, MaxCalls: 3, CurrentCalls: 1, Capabilities: []string{"vocabulary"}},
		{ID: "worker-b", Type: model.NodeTypeWorker, Endpoint: "worker://worker-b", Status: model.NodeStatusUp, MaxCalls: 2, CurrentCalls: 2, Capabilities: []string{"vocabulary"}},
		{ID: "worker-c", Type: model.NodeTypeWorker, Endpoint: "worker://worker-c", Status: model.NodeStatusSuspect, MaxCalls: 4, CurrentCalls: 1, Capabilities: []string{"vocabulary"}},
	} {
		if err := api.Registry.Register(context.Background(), node); err != nil {
			t.Fatalf("register node %s: %v", node.ID, err)
		}
	}

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeHTTPData[httpapi.SystemNodesResult](t, rec)
	if result.Media.Summary.Total != 2 || result.Media.Summary.Available != 1 || result.Media.Summary.Capacity != 120 || result.Media.Summary.CurrentCalls != 30 {
		t.Fatalf("unexpected media summary: %#v", result.Media.Summary)
	}
	if result.Workers.Summary.Total != 3 || result.Workers.Summary.Available != 1 || result.Workers.Summary.Unavailable != 2 {
		t.Fatalf("unexpected worker availability: %#v", result.Workers.Summary)
	}
	if result.Workers.Summary.Up != 2 || result.Workers.Summary.Suspect != 1 || result.Workers.Summary.Capacity != 9 || result.Workers.Summary.ActiveTasks != 4 {
		t.Fatalf("unexpected worker load summary: %#v", result.Workers.Summary)
	}
	if len(result.Workers.Nodes) != 3 || result.Workers.Nodes[0].ID != "worker-a" || result.Workers.Nodes[2].ID != "worker-c" {
		t.Fatalf("unexpected worker nodes: %#v", result.Workers.Nodes)
	}
	if result.Workers.Nodes[0].Endpoint != "worker://worker-a" || strings.Join(result.Workers.Nodes[0].Capabilities, ",") != "vocabulary" {
		t.Fatalf("unexpected worker node metadata: %#v", result.Workers.Nodes[0])
	}
}

func TestHTTP_InterpreterSessionList(t *testing.T) {
	api, store := newTestAPIWithSessionStore(t)
	seedInterpreterHistory(t, store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/interpreter/sessions?tenantId=tenant-a&limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeHTTPData[httpapi.InterpreterSessionListResult](t, rec)
	if result.Total != 2 || result.Limit != 1 || result.Offset != 0 || len(result.Items) != 1 {
		t.Fatalf("unexpected list result: %#v", result)
	}
	if result.Items[0].ID != "call-history-2" || result.Items[0].TenantID != "tenant-a" || result.Items[0].State != "ended" {
		t.Fatalf("unexpected first item: %#v", result.Items[0])
	}
}

func TestHTTP_InterpreterSessionDetail(t *testing.T) {
	api, store := newTestAPIWithSessionStore(t)
	seedInterpreterHistory(t, store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/interpreter/sessions/call-history-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeHTTPData[httpapi.InterpreterSessionDetailResult](t, rec)
	if result.Session.ID != "call-history-1" || result.Session.ProviderIDs["asr"][0] != 11 || !result.Session.DubbingEnabled {
		t.Fatalf("unexpected session summary: %#v", result.Session)
	}
	if len(result.Utterances) != 1 || result.Utterances[0].UtteranceID != "utt-1" || len(result.Utterances[0].ASRCallbacks) != 1 {
		t.Fatalf("unexpected utterances: %#v", result.Utterances)
	}
	finalASR := result.Utterances[0].ASRCallbacks[0]
	if !finalASR.IsFinal || finalASR.Text != "hello world" || finalASR.Metadata["requestId"] != "asr-final-1" {
		t.Fatalf("unexpected final asr: %#v", finalASR)
	}
	if len(finalASR.MTTranslations) != 1 || finalASR.MTTranslations[0].TargetText != "你好世界" {
		t.Fatalf("unexpected mt records: %#v", finalASR.MTTranslations)
	}
	if len(finalASR.LLMRevisions) != 1 || finalASR.LLMRevisions[0].RevisedText != "你好，世界" || !finalASR.LLMRevisions[0].Revised {
		t.Fatalf("unexpected llm records: %#v", finalASR.LLMRevisions)
	}
}

func TestHTTP_InterpreterSessionHistoryRequiresStore(t *testing.T) {
	api := newTestAPI()

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/interpreter/sessions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_InterpreterSessionDetailNotFound(t *testing.T) {
	api, _ := newTestAPIWithSessionStore(t)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/interpreter/sessions/missing-call", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_VocabularyTaskLifecycle(t *testing.T) {
	api, store := newTestAPIWithSessionStore(t)
	seedInterpreterHistory(t, store)

	first := doJSON(api, http.MethodPost, "/api/v1/interpreter/sessions/call-history-1/vocabulary-tasks", httpapi.CreateVocabularyTaskRequest{MaxWords: 20})
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", first.Code, first.Body.String())
	}
	firstResult := decodeHTTPData[httpapi.VocabularyTaskResult](t, first)
	if firstResult.SessionID != "call-history-1" || firstResult.TenantID != "tenant-a" || firstResult.UserID != "user-history" || firstResult.PartitionKey != "tenant-a:user-history" || firstResult.MaxWords != 20 {
		t.Fatalf("unexpected first task: %#v", firstResult)
	}

	second := doJSON(api, http.MethodPost, "/api/v1/interpreter/sessions/call-history-1/vocabulary-tasks", httpapi.CreateVocabularyTaskRequest{})
	if second.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", second.Code, second.Body.String())
	}
	secondResult := decodeHTTPData[httpapi.VocabularyTaskResult](t, second)
	if secondResult.MaxWords != sessionstore.DefaultVocabularyMaxWords {
		t.Fatalf("unexpected second task defaults: %#v", secondResult)
	}
	firstDetail, err := store.VocabularyTaskDetail(context.Background(), firstResult.ID)
	if err != nil {
		t.Fatalf("load first task: %v", err)
	}
	if firstDetail.Task.Status != sessionstore.VocabularyTaskStatusCancelled {
		t.Fatalf("first pending task should be cancelled, got %#v", firstDetail.Task)
	}

	list := httptest.NewRecorder()
	api.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/interpreter/sessions/call-history-1/vocabulary-tasks", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", list.Code, list.Body.String())
	}
	listResult := decodeHTTPData[httpapi.VocabularyTaskListResult](t, list)
	if listResult.Total != 2 || len(listResult.Items) != 2 {
		t.Fatalf("unexpected list result: %#v", listResult)
	}

	detail := httptest.NewRecorder()
	api.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/vocabulary-tasks/"+secondResult.ID, nil))
	if detail.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", detail.Code, detail.Body.String())
	}
	detailResult := decodeHTTPData[httpapi.VocabularyTaskDetailResult](t, detail)
	if detailResult.Task.ID != secondResult.ID {
		t.Fatalf("unexpected detail result: %#v", detailResult)
	}
}

func TestHTTP_VocabularyTaskValidationAndNotFound(t *testing.T) {
	api, _ := newTestAPIWithSessionStore(t)

	invalid := doJSON(api, http.MethodPost, "/api/v1/interpreter/sessions/missing/vocabulary-tasks", httpapi.CreateVocabularyTaskRequest{MaxWords: 101})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", invalid.Code, invalid.Body.String())
	}

	missing := doJSON(api, http.MethodPost, "/api/v1/interpreter/sessions/missing/vocabulary-tasks", httpapi.CreateVocabularyTaskRequest{})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", missing.Code, missing.Body.String())
	}
}

// 作用: 验证分机配置只保留查询能力。
func TestHTTP_ConfigExtension_QueryOnly(t *testing.T) {
	api := newTestAPI()
	ext := model.Extension{ID: "ext-1", TenantID: "tenant-a", Extension: "1001", Status: model.ExtensionStatusEnabled}

	if err := api.Config.SetExtension(context.Background(), ext); err != nil {
		t.Fatalf("seed extension: %v", err)
	}

	get := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/extensions/tenant-a/1001", nil)
	api.ServeHTTP(get, req)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "1001") {
		t.Fatalf("get failed %d: %s", get.Code, get.Body.String())
	}
}

// 作用: 验证 Test H T T P_ Response Wrapper_ Error Mapping 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestHTTP_ResponseWrapper_ErrorMapping(t *testing.T) {
	api := newTestAPI()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"message":"error"`) {
		t.Fatalf("expected error wrapper: %s", rec.Body.String())
	}
}

// 作用: 处理 new Test A P I 的核心流程。
func newTestAPI() *httpapi.API {
	client := etcdutil.NewMemoryClient()
	reg := registry.New(client, registry.Options{})
	return httpapi.New(httpapi.Dependencies{
		Registry: reg,
		Router:   router.New(reg),
		Config:   configcenter.New(client),
		Sessions: session.New(client),
	})
}

func newTestAPIWithSessionStore(t *testing.T) (*httpapi.API, *sessionstore.Store) {
	t.Helper()

	client := etcdutil.NewMemoryClient()
	reg := registry.New(client, registry.Options{})
	store, err := sessionstore.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	})
	if _, err := store.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("initialize sqlite store: %v", err)
	}
	return httpapi.New(httpapi.Dependencies{
		Registry:     reg,
		Router:       router.New(reg),
		Config:       configcenter.New(client),
		Sessions:     session.New(client),
		SessionStore: store,
	}), store
}

func seedInterpreterHistory(t *testing.T, store *sessionstore.Store) {
	t.Helper()
	ctx := context.Background()
	sessions := []sessionstore.InterpretSession{
		{
			ID:                "call-history-1",
			TenantID:          "tenant-a",
			ConnectionID:      "conn-history-1",
			UserID:            "user-history",
			State:             "active",
			ProviderIDsJSON:   `{"asr":[11],"mt":[12],"llm":[13]}`,
			TranslateStrategy: "hybrid",
			DubbingEnabled:    1,
			StartedAt:         "2026-06-06T02:00:00Z",
			MetadataJSON:      `{"client":"test"}`,
		},
		{
			ID:                "call-history-2",
			TenantID:          "tenant-a",
			ConnectionID:      "conn-history-2",
			UserID:            "user-history",
			State:             "ended",
			MediaState:        "ended",
			TranslateStrategy: "tmt",
			StartedAt:         "2026-06-06T03:00:00Z",
			EndedAt:           "2026-06-06T03:05:00Z",
		},
		{
			ID:        "call-history-3",
			TenantID:  "tenant-b",
			State:     "active",
			StartedAt: "2026-06-06T04:00:00Z",
		},
	}
	for _, session := range sessions {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create session %s: %v", session.ID, err)
		}
	}
	if _, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:   "call-history-1",
		CallID:      "call-history-1",
		UtteranceID: "utt-1",
		SequenceNo:  1,
		Language:    "en",
		Text:        "hello",
		IsFinal:     0,
	}); err != nil {
		t.Fatalf("insert partial asr: %v", err)
	}
	finalID, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:    "call-history-1",
		CallID:       "call-history-1",
		UtteranceID:  "utt-1",
		SequenceNo:   2,
		Language:     "en",
		Text:         "hello world",
		IsFinal:      1,
		MetadataJSON: `{"requestId":"asr-final-1"}`,
	})
	if err != nil {
		t.Fatalf("insert final asr: %v", err)
	}
	if _, err := store.InsertMTTranslation(ctx, sessionstore.MTTranslationRecord{
		ASRCallbackID: finalID,
		ProviderID:    12,
		ASRPhase:      "final",
		SourceText:    "hello world",
		TargetText:    "你好世界",
		IsFinal:       1,
		Status:        "ok",
	}); err != nil {
		t.Fatalf("insert mt translation: %v", err)
	}
	if _, err := store.InsertLLMRevision(ctx, sessionstore.LLMRevisionRecord{
		ASRCallbackID:    finalID,
		ProviderID:       13,
		SourceText:       "hello world",
		DraftTranslation: "你好世界",
		RevisedText:      "你好，世界",
		Revised:          1,
		Status:           "ok",
		ContextJSON:      `[{"sourceText":"previous sentence"}]`,
	}); err != nil {
		t.Fatalf("insert llm revision: %v", err)
	}
}

func decodeHTTPData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var response struct {
		Code  int    `json:"code"`
		Data  T      `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "" {
		t.Fatalf("unexpected error response: %s", response.Error)
	}
	return response.Data
}

// 作用: 处理 do J S O N 的核心流程。
func doJSON(api *httpapi.API, method, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	api.ServeHTTP(rec, req)
	return rec
}
