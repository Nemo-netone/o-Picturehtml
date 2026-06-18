// HTTP API处理器：REST接口的实现函数
package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/session"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
	"gorm.io/gorm"
)

func (api *API) handleNotFound(w http.ResponseWriter, r *http.Request) {
	JSONError(w, http.StatusNotFound, "not found")
}

func (api *API) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// handleHealth 返回控制中心基础健康状态。
// @Summary 健康检查
// @Description 返回 api-server HTTP API 是否可访问。
// @Tags system
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /api/v1/health [get]
func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, HealthResult{
		Status:  "ok",
		Service: "api-server",
		Time:    time.Now().UTC(),
	})
}

// handleSystemNodes 处理 GET /api/v1/system/nodes，用于统一观测 PBX media 和 worker 节点。
func (api *API) handleSystemNodes(w http.ResponseWriter, r *http.Request) {
	mediaNodes, err := api.mediaNodes(r)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	workerNodes, err := api.workerNodes(r)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, SystemNodesResult{
		RefreshedAt: time.Now().UTC(),
		Media: MediaNodesResult{
			Summary: summarizeMediaNodes(mediaNodes),
			Nodes:   mediaNodes,
		},
		Workers: WorkerNodesResult{
			Summary: summarizeWorkerNodes(workerNodes),
			Nodes:   workerNodes,
		},
	})
}

// handleMediaNodes 处理 GET /api/v1/media/nodes，用于观测当前在线 PBX media 节点与负载。
func (api *API) handleMediaNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := api.mediaNodes(r)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, MediaNodesResult{
		Summary: summarizeMediaNodes(nodes),
		Nodes:   nodes,
	})
}

// handleMediaPick 处理 GET /api/v1/media/pick，用于调试当前 media 节点选择结果。
func (api *API) handleMediaPick(w http.ResponseWriter, r *http.Request) {
	if api.MediaPicker == nil {
		JSONError(w, http.StatusServiceUnavailable, "media picker is not configured")
		return
	}
	node, err := api.MediaPicker.Pick(r.URL.Query().Get("policy"), r.URL.Query().Get("key"))
	if errors.Is(err, sdk.ErrNoNode) {
		JSONError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, node)
}

// handleListInterpreterSessions 处理 GET /api/v1/interpreter/sessions，查询历史同传会话。
// @Summary 查询历史同传会话
// @Description 按租户、状态和分页参数查询 api-server SQLite 中的同传会话记录。
// @Tags interpreter
// @Produce json
// @Param tenantId query string false "租户 ID"
// @Param state query string false "会话状态：active / ended / failed"
// @Param limit query int false "分页大小，默认 50，最大 200"
// @Param offset query int false "分页偏移，默认 0"
// @Success 200 {object} InterpreterSessionListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/interpreter/sessions [get]
func (api *API) handleListInterpreterSessions(w http.ResponseWriter, r *http.Request) {
	if api.SessionStore == nil {
		JSONError(w, http.StatusServiceUnavailable, "session store is not configured")
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := api.SessionStore.ListSessions(r.Context(), sessionstore.SessionListQuery{
		TenantID: r.URL.Query().Get("tenantId"),
		State:    r.URL.Query().Get("state"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]InterpreterSessionSummary, 0, len(result.Sessions))
	for _, session := range result.Sessions {
		items = append(items, interpreterSessionSummary(session))
	}
	JSON(w, http.StatusOK, InterpreterSessionListResult{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	})
}

// handleGetInterpreterSessionDetail 处理 GET /api/v1/interpreter/sessions/{callID}，查询单次会话完整明细。
// @Summary 查询单次同传会话明细
// @Description 返回 session 信息，并按 utterance 聚合 ASR callback 及其关联 TMT/LLM 记录。
// @Tags interpreter
// @Produce json
// @Param callID path string true "callId / interpret_sessions.id"
// @Success 200 {object} InterpreterSessionDetailResponse
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/interpreter/sessions/{callID} [get]
func (api *API) handleGetInterpreterSessionDetail(w http.ResponseWriter, r *http.Request) {
	if api.SessionStore == nil {
		JSONError(w, http.StatusServiceUnavailable, "session store is not configured")
		return
	}
	callID := chi.URLParam(r, "callID")
	detail, err := api.SessionStore.SessionDetail(r.Context(), callID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, interpreterSessionDetailResult(detail))
}

// handleCreateVocabularyTask 处理 POST /api/v1/interpreter/sessions/{callID}/vocabulary-tasks。
func (api *API) handleCreateVocabularyTask(w http.ResponseWriter, r *http.Request) {
	if api.SessionStore == nil {
		JSONError(w, http.StatusServiceUnavailable, "session store is not configured")
		return
	}
	callID := chi.URLParam(r, "callID")
	var req CreateVocabularyTaskRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.MaxWords < 0 {
		JSONError(w, http.StatusBadRequest, "maxWords must be a positive integer")
		return
	}
	if req.MaxWords > sessionstore.MaxVocabularyMaxWords {
		JSONError(w, http.StatusBadRequest, "maxWords must be <= 100")
		return
	}
	task, err := api.SessionStore.CreateVocabularyTask(r.Context(), sessionstore.VocabularyTask{
		ID:            idgen.WorkerID(),
		SessionID:     callID,
		MaxWords:      req.MaxWords,
		EnglishSource: sessionstore.DefaultVocabularySource,
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusAccepted, vocabularyTaskResult(task))
}

// handleListVocabularyTasks 处理 GET /api/v1/interpreter/sessions/{callID}/vocabulary-tasks。
func (api *API) handleListVocabularyTasks(w http.ResponseWriter, r *http.Request) {
	if api.SessionStore == nil {
		JSONError(w, http.StatusServiceUnavailable, "session store is not configured")
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := api.SessionStore.ListVocabularyTasks(r.Context(), sessionstore.VocabularyTaskListQuery{
		SessionID: chi.URLParam(r, "callID"),
		Status:    r.URL.Query().Get("status"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]VocabularyTaskResult, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		items = append(items, vocabularyTaskResult(task))
	}
	JSON(w, http.StatusOK, VocabularyTaskListResult{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	})
}

// handleGetVocabularyTask 处理 GET /api/v1/vocabulary-tasks/{taskID}。
func (api *API) handleGetVocabularyTask(w http.ResponseWriter, r *http.Request) {
	if api.SessionStore == nil {
		JSONError(w, http.StatusServiceUnavailable, "session store is not configured")
		return
	}
	detail, err := api.SessionStore.VocabularyTaskDetail(r.Context(), chi.URLParam(r, "taskID"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries := make([]VocabularyEntryResult, 0, len(detail.Entries))
	for _, entry := range detail.Entries {
		entries = append(entries, vocabularyEntryResult(entry))
	}
	JSON(w, http.StatusOK, VocabularyTaskDetailResult{
		Task:    vocabularyTaskResult(detail.Task),
		Entries: entries,
	})
}

func (api *API) mediaNodes(r *http.Request) ([]*model.Node, error) {
	if source, ok := api.MediaPicker.(MediaNodeSource); ok && source != nil {
		nodes := source.Nodes()
		sortMediaNodes(nodes)
		return nodes, nil
	}
	if api.Registry == nil {
		return nil, nil
	}
	nodes, err := api.Registry.ListNodes(r.Context(), model.NodeTypeMedia)
	if err != nil {
		return nil, err
	}
	sortMediaNodes(nodes)
	return nodes, nil
}

func (api *API) workerNodes(r *http.Request) ([]*model.Node, error) {
	if api.Registry == nil {
		return nil, nil
	}
	nodes, err := api.Registry.ListNodes(r.Context(), model.NodeTypeWorker)
	if err != nil {
		return nil, err
	}
	sortMediaNodes(nodes)
	return nodes, nil
}

func summarizeMediaNodes(nodes []*model.Node) MediaNodeSummary {
	summary := MediaNodeSummary{Total: len(nodes)}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch node.Status {
		case model.NodeStatusUp:
			summary.Up++
		case model.NodeStatusDown:
			summary.Down++
		case model.NodeStatusDraining:
			summary.Draining++
		case model.NodeStatusSuspect:
			summary.Suspect++
		}
		if mediaNodeAvailable(node) {
			summary.Available++
		}
		if node.MaxCalls > 0 {
			summary.Capacity += node.MaxCalls
		}
		summary.CurrentCalls += node.CurrentCalls
	}
	summary.Unavailable = summary.Total - summary.Available
	return summary
}

func summarizeWorkerNodes(nodes []*model.Node) WorkerNodeSummary {
	summary := WorkerNodeSummary{Total: len(nodes)}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch node.Status {
		case model.NodeStatusUp:
			summary.Up++
		case model.NodeStatusDown:
			summary.Down++
		case model.NodeStatusDraining:
			summary.Draining++
		case model.NodeStatusSuspect:
			summary.Suspect++
		}
		if workerNodeAvailable(node) {
			summary.Available++
		}
		if node.MaxCalls > 0 {
			summary.Capacity += node.MaxCalls
		}
		summary.ActiveTasks += node.CurrentCalls
	}
	summary.Unavailable = summary.Total - summary.Available
	return summary
}

func mediaNodeAvailable(node *model.Node) bool {
	if node == nil || node.Status != model.NodeStatusUp {
		return false
	}
	return node.MaxCalls <= 0 || node.CurrentCalls < node.MaxCalls
}

func workerNodeAvailable(node *model.Node) bool {
	if node == nil || node.Status != model.NodeStatusUp {
		return false
	}
	return node.MaxCalls <= 0 || node.CurrentCalls < node.MaxCalls
}

func sortMediaNodes(nodes []*model.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i] == nil {
			return false
		}
		if nodes[j] == nil {
			return true
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func interpreterSessionSummary(session sessionstore.InterpretSession) InterpreterSessionSummary {
	return InterpreterSessionSummary{
		ID:                session.ID,
		TenantID:          session.TenantID,
		ConnectionID:      session.ConnectionID,
		UserID:            session.UserID,
		Caller:            session.Caller,
		Callee:            session.Callee,
		State:             session.State,
		MediaState:        session.MediaState,
		ProviderIDs:       decodeProviderIDMap(session.ProviderIDsJSON),
		TranslateStrategy: session.TranslateStrategy,
		DubbingEnabled:    session.DubbingEnabled == 1,
		StartedAt:         session.StartedAt,
		EndedAt:           session.EndedAt,
		CreatedAt:         session.CreatedAt,
		UpdatedAt:         session.UpdatedAt,
		Metadata:          decodeJSONObject(session.MetadataJSON),
	}
}

func interpreterSessionDetailResult(detail sessionstore.SessionDetail) InterpreterSessionDetailResult {
	mtByASR := map[int64][]InterpreterMTRecordResult{}
	for _, record := range detail.MTTranslations {
		mtByASR[record.ASRCallbackID] = append(mtByASR[record.ASRCallbackID], interpreterMTRecordResult(record))
	}
	llmByASR := map[int64][]InterpreterLLMRecordResult{}
	for _, record := range detail.LLMRevisions {
		llmByASR[record.ASRCallbackID] = append(llmByASR[record.ASRCallbackID], interpreterLLMRecordResult(record))
	}

	utteranceIndex := map[string]int{}
	utterances := make([]InterpreterUtteranceDetail, 0)
	for _, callback := range detail.ASRCallbacks {
		utteranceID := callback.UtteranceID
		index, ok := utteranceIndex[utteranceID]
		if !ok {
			index = len(utterances)
			utteranceIndex[utteranceID] = index
			utterances = append(utterances, InterpreterUtteranceDetail{UtteranceID: utteranceID})
		}
		asr := interpreterASRCallbackResult(callback)
		asr.MTTranslations = mtByASR[callback.ID]
		asr.LLMRevisions = llmByASR[callback.ID]
		utterances[index].ASRCallbacks = append(utterances[index].ASRCallbacks, asr)
	}

	return InterpreterSessionDetailResult{
		Session:    interpreterSessionSummary(detail.Session),
		Utterances: utterances,
	}
}

func interpreterASRCallbackResult(callback sessionstore.ASRCallback) InterpreterASRCallbackResult {
	return InterpreterASRCallbackResult{
		ID:          callback.ID,
		SessionID:   callback.SessionID,
		ProviderID:  callback.ProviderID,
		CallID:      callback.CallID,
		UtteranceID: callback.UtteranceID,
		SequenceNo:  callback.SequenceNo,
		Language:    callback.Language,
		Text:        callback.Text,
		IsFinal:     callback.IsFinal == 1,
		Confidence:  callback.Confidence,
		StartMS:     callback.StartMS,
		EndMS:       callback.EndMS,
		ReceivedAt:  callback.ReceivedAt,
		Metadata:    decodeJSONObject(callback.MetadataJSON),
		RawJSON:     callback.RawJSON,
	}
}

func interpreterMTRecordResult(record sessionstore.MTTranslationRecord) InterpreterMTRecordResult {
	return InterpreterMTRecordResult{
		ID:              record.ID,
		ASRCallbackID:   record.ASRCallbackID,
		ProviderID:      record.ProviderID,
		ASRPhase:        record.ASRPhase,
		SourceLang:      record.SourceLang,
		TargetLang:      record.TargetLang,
		SourceText:      record.SourceText,
		TargetText:      record.TargetText,
		IsFinal:         record.IsFinal == 1,
		Status:          record.Status,
		ErrorCode:       record.ErrorCode,
		ErrorMessage:    record.ErrorMessage,
		LatencyMS:       record.LatencyMS,
		RequestedAt:     record.RequestedAt,
		RespondedAt:     record.RespondedAt,
		Metadata:        decodeJSONObject(record.MetadataJSON),
		RawRequestJSON:  record.RawRequestJSON,
		RawResponseJSON: record.RawResponseJSON,
	}
}

func interpreterLLMRecordResult(record sessionstore.LLMRevisionRecord) InterpreterLLMRecordResult {
	return InterpreterLLMRecordResult{
		ID:               record.ID,
		ASRCallbackID:    record.ASRCallbackID,
		ProviderID:       record.ProviderID,
		SourceText:       record.SourceText,
		DraftTranslation: record.DraftTranslation,
		RevisedText:      record.RevisedText,
		Revised:          record.Revised == 1,
		Status:           record.Status,
		ErrorMessage:     record.ErrorMessage,
		LatencyMS:        record.LatencyMS,
		RequestedAt:      record.RequestedAt,
		RespondedAt:      record.RespondedAt,
		Context:          decodeJSONAny(record.ContextJSON),
		Terms:            decodeJSONAny(record.TermsJSON),
		Metadata:         decodeJSONObject(record.MetadataJSON),
		RawRequestJSON:   record.RawRequestJSON,
		RawResponseJSON:  record.RawResponseJSON,
	}
}

func vocabularyTaskResult(task sessionstore.VocabularyTask) VocabularyTaskResult {
	return VocabularyTaskResult{
		ID:            task.ID,
		SessionID:     task.SessionID,
		TenantID:      task.TenantID,
		UserID:        task.UserID,
		PartitionKey:  task.PartitionKey,
		Status:        task.Status,
		MaxWords:      task.MaxWords,
		EnglishSource: task.EnglishSource,
		AttemptCount:  task.AttemptCount,
		LockedBy:      task.LockedBy,
		LockedAt:      task.LockedAt,
		StartedAt:     task.StartedAt,
		FinishedAt:    task.FinishedAt,
		ErrorMessage:  task.ErrorMessage,
		Input:         decodeJSONAny(task.InputJSON),
		CreatedAt:     task.CreatedAt,
		UpdatedAt:     task.UpdatedAt,
	}
}

func vocabularyEntryResult(entry sessionstore.VocabularyEntry) VocabularyEntryResult {
	return VocabularyEntryResult{
		ID:                 entry.ID,
		TaskID:             entry.TaskID,
		Ordinal:            entry.Ordinal,
		Word:               entry.Word,
		Lemma:              entry.Lemma,
		Phonetic:           entry.Phonetic,
		PartOfSpeech:       entry.PartOfSpeech,
		MeaningZH:          entry.MeaningZH,
		ExampleEN:          entry.ExampleEN,
		ExampleZH:          entry.ExampleZH,
		Occurrences:        entry.Occurrences,
		Difficulty:         entry.Difficulty,
		SourceUtteranceIDs: decodeJSONAny(entry.SourceUtteranceIDsJSON),
		Metadata:           decodeJSONObject(entry.MetadataJSON),
	}
}

// handleCallRoute 处理 POST /api/v1/calls/route，路由新通话并自动创建 session。
// @Summary 路由新通话
// @Description 按租户、媒体需求和路由策略选择媒体节点；需要 AI 时同时组装 AI pipeline 并创建通话 session。
// @Tags calls
// @Accept json
// @Produce json
// @Param request body RouteCallRequest true "路由请求"
// @Success 200 {object} RouteCallResponse
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/calls/route [post]
func (api *API) handleCallRoute(w http.ResponseWriter, r *http.Request) {
	var req router.RouteRequest
	if err := decodeJSON(r, &req); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := api.Router.Route(r.Context(), req)
	if errors.Is(err, router.ErrNoAvailableNode) {
		JSONError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if api.Sessions != nil {
		_, _ = api.Sessions.CreateSession(r.Context(), result.CallID, session.CreateRequest{
			TenantID:    req.TenantID,
			Caller:      req.Caller,
			Callee:      req.Callee,
			OwnerNode:   result.MediaNode,
			GatewayNode: result.GatewayNode,
			MediaNode:   result.MediaNode,
			TurnNode:    result.TurnNode,
			AIPipeline:  result.AIPipeline,
		})
	}
	JSON(w, http.StatusOK, result)
}

// handleGetCall 处理 GET /api/v1/calls/{callID}。
// @Summary 查询通话
// @Description 根据 callID 查询通话 session 当前状态。
// @Tags calls
// @Produce json
// @Param callID path string true "通话 ID"
// @Success 200 {object} CallSessionResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/calls/{callID} [get]
func (api *API) handleGetCall(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "callID")
	call, err := api.Sessions.GetSession(r.Context(), callID)
	if err != nil {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, call)
}

// handleEndCall 处理 DELETE /api/v1/calls/{callID}，结束通话。
// @Summary 结束通话
// @Description 根据当前 session epoch 结束指定通话。
// @Tags calls
// @Produce json
// @Param callID path string true "通话 ID"
// @Success 200 {object} CallEndResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/calls/{callID} [delete]
func (api *API) handleEndCall(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "callID")
	call, err := api.Sessions.GetSession(r.Context(), callID)
	if err != nil {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := api.Sessions.EndSession(r.Context(), callID, call.Owner.Epoch); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	api.endStoredInterpretSession(r.Context(), callID, "")
	JSON(w, http.StatusOK, map[string]string{"callId": callID})
}

// handleGetExtension 处理 GET /api/v1/config/extensions/{tenant}/{ext}。
// @Summary 查询分机配置
// @Description 根据租户 ID 和分机号查询分机配置。
// @Tags config
// @Produce json
// @Param tenantID path string true "租户 ID"
// @Param extension path string true "分机号"
// @Success 200 {object} ExtensionResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/config/extensions/{tenantID}/{extension} [get]
func (api *API) handleGetExtension(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	extension := chi.URLParam(r, "extension")
	ext, err := api.Config.GetExtension(r.Context(), tenantID, extension)
	if errors.Is(err, configcenter.ErrConfigNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, ext)
}

// handleGetPresence 处理 GET /api/v1/extensions/{tenant}/{ext}/presence。
// @Summary 查询分机在线状态
// @Description 根据租户 ID 和分机号查询 presence 状态。
// @Tags extensions
// @Produce json
// @Param tenantID path string true "租户 ID"
// @Param extension path string true "分机号"
// @Success 200 {object} PresenceResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/extensions/{tenantID}/{extension}/presence [get]
func (api *API) handleGetPresence(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	extension := chi.URLParam(r, "extension")
	ext, err := api.Config.GetExtension(r.Context(), tenantID, extension)
	if err != nil || ext.Presence == nil {
		JSONError(w, http.StatusNotFound, "presence not found")
		return
	}
	JSON(w, http.StatusOK, ext.Presence)
}
