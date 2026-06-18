// 同传会话核心：ASR记录→ TMT转发→ DeepSeek Flash纠错→ TTS触发→ 字幕状态机
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

const contextWindowSize = 6

type contextEntry struct {
	SourceText string `json:"sourceText"`
}

type utteranceState struct {
	ASRFinalID            int64
	ASRFinalIDReady       chan struct{}
	ASRFinalIDResolved    bool
	ASRFinalInsertStarted bool
	ASRText               string
	SourceContext         []contextEntry
	TMTFinal              string
	DeepSeekText          string
	DeepSeekSent          bool
	LLMStarted            bool
	TTSOrder              int
	TTSSelected           bool
}

type interpreterSession struct {
	mu            sync.Mutex
	api           *API
	connectionID  string
	callID        string
	userID        string
	tenantID      string
	strategy      string
	sequenceNo    int64
	asrIDs        map[string]int64
	utterances    map[string]*utteranceState
	sourceContext []contextEntry
	providerIDs   map[string]int64
	ttsOrderSeq   int
	nextTTSSend   int
	ttsReady      map[int]ttsDispatchTask
	ttsNotify     chan struct{}
	ttsStop       chan struct{}
	ttsStopOnce   sync.Once
	storeWG       sync.WaitGroup
	language      model.SessionLanguageOptions
}

type revisionTask struct {
	UtteranceID string
	ASRID       int64
	ASRIDReady  <-chan struct{}
	SourceText  string
	Context     []contextEntry
	Language    model.SessionLanguageOptions
}

type ttsDispatchTask struct {
	Sequence     int
	ConnectionID string
	CallID       string
	UserID       string
	UtteranceID  string
	Text         string
	Voice        string
	Language     string
	Metadata     map[string]string
	Skip         bool
}

func (api *API) createInterpreterSession(ctx context.Context, connectionID, callID, userID, tenantID, strategy string, dubbing bool, languages ...model.SessionLanguageOptions) *interpreterSession {
	language := model.DefaultSessionLanguageOptions()
	if len(languages) > 0 {
		language = languages[0].WithDefaults()
	}
	session := &interpreterSession{
		api:          api,
		connectionID: connectionID,
		callID:       callID,
		userID:       userID,
		tenantID:     tenantID,
		strategy:     effectiveTranslateStrategy(strategy),
		asrIDs:       map[string]int64{},
		utterances:   map[string]*utteranceState{},
		providerIDs:  map[string]int64{},
		nextTTSSend:  1,
		ttsReady:     map[int]ttsDispatchTask{},
		ttsNotify:    make(chan struct{}, 1),
		ttsStop:      make(chan struct{}),
		language:     language,
	}
	api.mu.Lock()
	api.interpreters[connectionID] = session
	api.mu.Unlock()
	session.initializeStore(ctx, dubbing)
	go session.runTTSCoordinator()
	return session
}

func (api *API) interpreter(connectionID string) *interpreterSession {
	api.mu.RLock()
	defer api.mu.RUnlock()
	return api.interpreters[connectionID]
}

func (s *interpreterSession) initializeStore(ctx context.Context, dubbing bool) {
	if s.api.SessionStore == nil {
		return
	}
	if s.tenantID == "" {
		s.tenantID = "default"
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"language": s.language.Metadata(),
	})
	if err := s.api.SessionStore.CreateSession(ctx, sessionstore.InterpretSession{
		ID:                s.callID,
		TenantID:          s.tenantID,
		ConnectionID:      s.connectionID,
		UserID:            s.userID,
		State:             "active",
		ProviderIDsJSON:   "{}",
		TranslateStrategy: s.strategy,
		DubbingEnabled:    boolToInt(dubbing),
		MetadataJSON:      string(metadataJSON),
	}); err != nil {
		slog.WarnContext(ctx, "创建同传会话存储记录失败",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.Any("error", err),
		)
	}
	if s.api.LLM != nil {
		id := s.upsertProvider(ctx, "llm", model.ProviderConfig{Provider: s.api.LLM.Provider()})
		if id > 0 {
			s.providerIDs["llm"] = id
		}
	}
}

func (s *interpreterSession) endStore(ctx context.Context) {
	s.api.endStoredInterpretSession(ctx, s.callID, s.connectionID)
}

func (api *API) endStoredInterpretSession(ctx context.Context, callID, connectionID string) {
	if api.SessionStore == nil || strings.TrimSpace(callID) == "" {
		return
	}
	if err := api.SessionStore.EndSession(ctx, callID); err != nil {
		slog.WarnContext(ctx, "结束同传会话存储记录失败",
			slog.String("connectionId", connectionID),
			slog.String("callId", callID),
			slog.Any("error", err),
		)
		return
	}
	slog.InfoContext(ctx, "同传会话存储记录已结束",
		slog.String("connectionId", connectionID),
		slog.String("callId", callID),
	)
}

func (s *interpreterSession) ensureProviderSummary(ctx context.Context, capability, provider string) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return
	}
	s.mu.Lock()
	if s.providerIDs[capability] > 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	id := s.upsertProvider(ctx, capability, model.ProviderConfig{Provider: provider})
	if id <= 0 {
		return
	}
	s.mu.Lock()
	if s.providerIDs[capability] == 0 {
		s.providerIDs[capability] = id
	}
	s.mu.Unlock()
}

func (s *interpreterSession) upsertProvider(ctx context.Context, capability string, config model.ProviderConfig) int64 {
	if s.api.SessionStore == nil || strings.TrimSpace(config.Provider) == "" {
		return 0
	}
	paramsJSON, _ := json.Marshal(config.Params)
	metadataJSON, _ := json.Marshal(map[string]any{
		"secretsConfigured": len(config.Secrets) > 0,
	})
	id, err := s.api.SessionStore.UpsertProvider(ctx, sessionstore.Provider{
		Name:         config.Provider,
		Capability:   capability,
		EndpointURL:  config.Endpoint,
		Model:        config.Params["model"],
		Enabled:      1,
		ConfigJSON:   string(paramsJSON),
		MetadataJSON: string(metadataJSON),
	})
	if err != nil {
		slog.WarnContext(ctx, "写入 provider 记录失败", slog.String("capability", capability), slog.Any("error", err))
		return 0
	}
	if err := s.api.SessionStore.AddProviderToSession(ctx, s.callID, capability, id); err != nil {
		slog.WarnContext(ctx, "关联 provider 到同传会话失败", slog.String("capability", capability), slog.Int64("providerId", id), slog.Any("error", err))
	}
	return id
}

func (api *API) processASRResult(ctx context.Context, conn *wsConn, connectionID, userID string, result model.ASRResult) {
	_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
		Type:         "asr_result",
		ConnectionID: connectionID,
		CallID:       result.CallID,
		UserID:       userID,
		UtteranceID:  result.UtteranceID,
		Text:         result.Text,
		IsFinal:      result.IsFinal,
		Confidence:   result.Confidence,
		Language:     result.Language,
		Metadata:     map[string]string{"utteranceId": result.UtteranceID},
	})
	session := api.interpreter(connectionID)
	if session == nil {
		return
	}
	session.recordASR(ctx, result)
	if result.IsFinal && session.aiEnabled() {
		go session.runLLMRevision(ctx, conn, result.UtteranceID)
	}
}

func (s *interpreterSession) recordASR(ctx context.Context, result model.ASRResult) {
	s.mu.Lock()
	s.sequenceNo++
	sequenceNo := s.sequenceNo
	providerID := s.providerIDs["asr"]
	shouldInsert := s.api.SessionStore != nil
	var finalReady chan struct{}
	if result.IsFinal {
		state := s.utterance(result.UtteranceID)
		text := strings.TrimSpace(result.Text)
		if state.ASRText == "" && text != "" {
			state.SourceContext = append([]contextEntry(nil), s.sourceContext...)
			s.appendSourceContext(contextEntry{SourceText: text})
		}
		state.ASRText = text
		if state.TTSOrder == 0 {
			s.ttsOrderSeq++
			state.TTSOrder = s.ttsOrderSeq
		}
		if shouldInsert && !state.ASRFinalInsertStarted {
			state.ASRFinalInsertStarted = true
			finalReady = s.ensureASRFinalIDReadyLocked(state)
		} else {
			shouldInsert = false
		}
	}
	s.mu.Unlock()

	if !shouldInsert {
		return
	}
	record := sessionstore.ASRCallback{
		SessionID:   s.callID,
		ProviderID:  providerID,
		CallID:      result.CallID,
		UtteranceID: result.UtteranceID,
		SequenceNo:  sequenceNo,
		Language:    result.Language,
		Text:        result.Text,
		IsFinal:     boolToInt(result.IsFinal),
		Confidence:  result.Confidence,
		StartMS:     result.StartMs,
		EndMS:       result.EndMs,
	}
	s.runStoreTask(func() {
		s.insertASRCallback(context.WithoutCancel(ctx), result.UtteranceID, record, finalReady)
	})
}

func (s *interpreterSession) insertASRCallback(ctx context.Context, utteranceID string, record sessionstore.ASRCallback, ready chan struct{}) {
	inserted, err := s.api.SessionStore.InsertASRCallback(ctx, record)
	if err != nil {
		slog.WarnContext(ctx, "写入 ASR 回调记录失败",
			slog.String("callId", record.CallID),
			slog.String("utteranceId", utteranceID),
			slog.Any("error", err),
		)
	}
	if ready != nil {
		s.completeASRCallbackID(utteranceID, inserted, ready)
	}
}

func (s *interpreterSession) ensureASRFinalIDReadyLocked(state *utteranceState) chan struct{} {
	if state.ASRFinalID > 0 || state.ASRFinalIDResolved {
		return nil
	}
	if state.ASRFinalIDReady == nil {
		state.ASRFinalIDReady = make(chan struct{})
	}
	return state.ASRFinalIDReady
}

func (s *interpreterSession) completeASRCallbackID(utteranceID string, id int64, ready chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.utterance(utteranceID)
	if id > 0 {
		state.ASRFinalID = id
		s.asrIDs[utteranceID] = id
	}
	if state.ASRFinalIDReady == ready && !state.ASRFinalIDResolved {
		state.ASRFinalIDResolved = true
		close(ready)
	}
}

func (s *interpreterSession) resolveASRCallbackID(ctx context.Context, utteranceID string, id int64, ready <-chan struct{}) int64 {
	if id > 0 {
		return id
	}
	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return 0
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored := s.asrIDs[utteranceID]; stored > 0 {
		return stored
	}
	if state := s.utterances[utteranceID]; state != nil {
		return state.ASRFinalID
	}
	return 0
}

func (api *API) processTranslationResult(ctx context.Context, conn *wsConn, connectionID, userID string, result model.TranslationResult) {
	session := api.interpreter(connectionID)
	if session == nil {
		api.writeTranslationResult(ctx, conn, connectionID, userID, result)
		return
	}
	shouldForward := session.recordTMT(ctx, result)
	if shouldForward {
		api.writeTranslationResult(ctx, conn, connectionID, userID, result)
		session.queueAutoTTS(ctx, conn, result)
	}
}

func (s *interpreterSession) recordTMT(ctx context.Context, result model.TranslationResult) bool {
	s.mu.Lock()
	state := s.utterance(result.UtteranceID)
	asrID := state.ASRFinalID
	if asrID == 0 {
		asrID = s.asrIDs[result.UtteranceID]
	}
	asrIDReady := state.ASRFinalIDReady
	providerID := s.providerIDs["tmt"]
	shouldForward := true
	if result.IsFinal {
		state.TMTFinal = strings.TrimSpace(result.Text)
	}
	s.mu.Unlock()

	if s.api.SessionStore != nil && (asrID > 0 || asrIDReady != nil) {
		phase := "partial"
		if result.IsFinal {
			phase = "final"
		}
		record := sessionstore.MTTranslationRecord{
			ProviderID: providerID,
			ASRPhase:   phase,
			SourceLang: s.language.SourceLanguage,
			TargetLang: s.language.TargetLanguage,
			SourceText: result.SourceText,
			TargetText: result.Text,
			IsFinal:    boolToInt(result.IsFinal),
			Status:     "ok",
		}
		s.runStoreTask(func() {
			s.insertMTTranslation(context.WithoutCancel(ctx), result.UtteranceID, asrID, asrIDReady, record)
		})
	}
	return shouldForward
}

func (s *interpreterSession) runLLMRevision(ctx context.Context, conn *wsConn, utteranceID string) {
	if s.api.LLM == nil {
		return
	}
	var task revisionTask
	s.mu.Lock()
	state := s.utterances[utteranceID]
	if state != nil && strings.TrimSpace(state.ASRText) != "" && !state.LLMStarted {
		state.LLMStarted = true
		task.UtteranceID = utteranceID
		task.ASRID = state.ASRFinalID
		task.ASRIDReady = state.ASRFinalIDReady
		task.SourceText = state.ASRText
		task.Context = append([]contextEntry(nil), state.SourceContext...)
		task.Language = s.language
	}
	s.mu.Unlock()
	if task.UtteranceID == "" || task.SourceText == "" {
		return
	}
	started := time.Now()
	result, err := s.api.LLM.Translate(ctx, llm.Request{
		CallID:        s.callID,
		UtteranceID:   task.UtteranceID,
		Text:          task.SourceText,
		SourceLang:    task.Language.SourceLanguage,
		TargetLang:    task.Language.TargetLanguage,
		Quality:       true,
		SourceContext: sourceContextLines(task.Context),
	})
	latencyMS := time.Since(started).Milliseconds()
	if err != nil {
		s.insertLLMError(ctx, task, err, latencyMS)
		return
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return
	}
	s.mu.Lock()
	state = s.utterance(task.UtteranceID)
	draft := state.TMTFinal
	revised := true
	if draft != "" {
		revised = normalizeComparable(text) != normalizeComparable(draft)
	}
	state.DeepSeekText = text
	state.DeepSeekSent = true
	s.mu.Unlock()
	s.insertLLMSuccess(ctx, task, text, revised, result.Terms, latencyMS)
	translation := model.TranslationResult{
		CallID:      s.callID,
		UtteranceID: task.UtteranceID,
		SourceText:  task.SourceText,
		Text:        text,
		IsFinal:     true,
		Engine:      "deepseek-flash",
		Revised:     revised,
		Language:    task.Language.TargetLanguage,
	}
	s.api.writeTranslationResult(ctx, conn, s.connectionID, s.userID, translation)
	s.queueAutoTTS(ctx, conn, translation)
}

func (s *interpreterSession) insertMTTranslation(ctx context.Context, utteranceID string, asrID int64, ready <-chan struct{}, record sessionstore.MTTranslationRecord) {
	record.ASRCallbackID = s.resolveASRCallbackID(ctx, utteranceID, asrID, ready)
	if record.ASRCallbackID <= 0 {
		slog.WarnContext(ctx, "跳过 TMT 翻译记录写入：缺少 ASR callback id",
			slog.String("callId", s.callID),
			slog.String("utteranceId", utteranceID),
		)
		return
	}
	_, err := s.api.SessionStore.InsertMTTranslation(ctx, record)
	if err != nil {
		slog.WarnContext(ctx, "写入 TMT 翻译记录失败",
			slog.String("callId", s.callID),
			slog.String("utteranceId", utteranceID),
			slog.Int64("asrCallbackId", record.ASRCallbackID),
			slog.Any("error", err),
		)
	}
}

func (s *interpreterSession) insertLLMSuccess(ctx context.Context, task revisionTask, text string, revised bool, terms map[string]string, latencyMS int64) {
	if s.api.SessionStore == nil {
		return
	}
	contextJSON, _ := json.Marshal(task.Context)
	termsJSON, _ := json.Marshal(terms)
	metadataJSON, _ := json.Marshal(map[string]any{
		"engine":         "deepseek-flash",
		"sourceLanguage": task.Language.SourceLanguage,
		"targetLanguage": task.Language.TargetLanguage,
	})
	record := sessionstore.LLMRevisionRecord{
		ProviderID:   s.providerIDs["llm"],
		SourceText:   task.SourceText,
		RevisedText:  text,
		Revised:      boolToInt(revised),
		Status:       "ok",
		LatencyMS:    latencyMS,
		RespondedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ContextJSON:  string(contextJSON),
		TermsJSON:    string(termsJSON),
		MetadataJSON: string(metadataJSON),
	}
	s.runStoreTask(func() {
		s.insertLLMRevision(context.WithoutCancel(ctx), task, record)
	})
}

func (s *interpreterSession) insertLLMRevision(ctx context.Context, task revisionTask, record sessionstore.LLMRevisionRecord) {
	record.ASRCallbackID = s.resolveASRCallbackID(ctx, task.UtteranceID, task.ASRID, task.ASRIDReady)
	if record.ASRCallbackID <= 0 {
		slog.WarnContext(ctx, "跳过 LLM 校准记录写入：缺少 ASR callback id",
			slog.String("callId", s.callID),
			slog.String("utteranceId", task.UtteranceID),
		)
		return
	}
	_, err := s.api.SessionStore.InsertLLMRevision(ctx, record)
	if err != nil {
		slog.WarnContext(ctx, "写入 LLM 校准记录失败", slog.String("callId", s.callID), slog.String("utteranceId", task.UtteranceID), slog.Any("error", err))
	}
}

func (s *interpreterSession) insertLLMError(ctx context.Context, task revisionTask, err error, latencyMS int64) {
	if s.api.SessionStore == nil {
		return
	}
	contextJSON, _ := json.Marshal(task.Context)
	metadataJSON, _ := json.Marshal(map[string]any{
		"engine":         "deepseek-flash",
		"sourceLanguage": task.Language.SourceLanguage,
		"targetLanguage": task.Language.TargetLanguage,
	})
	record := sessionstore.LLMRevisionRecord{
		ProviderID:   s.providerIDs["llm"],
		SourceText:   task.SourceText,
		Status:       "error",
		ErrorMessage: err.Error(),
		LatencyMS:    latencyMS,
		RespondedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ContextJSON:  string(contextJSON),
		MetadataJSON: string(metadataJSON),
	}
	s.runStoreTask(func() {
		s.insertLLMRevision(context.WithoutCancel(ctx), task, record)
	})
}

func (s *interpreterSession) runStoreTask(fn func()) {
	s.storeWG.Add(1)
	go func() {
		defer s.storeWG.Done()
		fn()
	}()
}

func (s *interpreterSession) waitStoreTasks() {
	s.storeWG.Wait()
}

func (s *interpreterSession) aiEnabled() bool {
	switch s.strategy {
	case "hybrid", "deepseek", "llm":
		return true
	default:
		return false
	}
}

func (s *interpreterSession) queueAutoTTS(ctx context.Context, conn *wsConn, result model.TranslationResult) {
	if !result.IsFinal || strings.TrimSpace(result.Text) == "" {
		return
	}
	if !conn.dubbingEnabled() {
		if s.markAutoTTSSkipped(result.UtteranceID) {
			s.notifyTTSCoordinator()
		}
		return
	}
	if s.reserveAutoTTS(result) {
		s.notifyTTSCoordinator()
	}
}

func (s *interpreterSession) reserveAutoTTS(result model.TranslationResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTTSCoordinatorStateLocked()
	state := s.utterance(result.UtteranceID)
	if state.TTSOrder <= 0 || state.TTSSelected {
		return false
	}
	language := s.language.WithDefaults()
	state.TTSSelected = true
	s.ttsReady[state.TTSOrder] = ttsDispatchTask{
		Sequence:     state.TTSOrder,
		ConnectionID: s.connectionID,
		CallID:       result.CallID,
		UserID:       s.userID,
		UtteranceID:  result.UtteranceID,
		Text:         strings.TrimSpace(result.Text),
		Voice:        language.TTSVoiceType,
		Language:     language.TTSLanguage,
		Metadata: map[string]string{
			model.LanguageMetadataSourceLanguage:     language.SourceLanguage,
			model.LanguageMetadataTargetLanguage:     language.TargetLanguage,
			model.LanguageMetadataTTSPrimaryLanguage: strconv.Itoa(language.TTSPrimaryLanguage),
		},
	}
	return true
}

func (s *interpreterSession) markAutoTTSSkipped(utteranceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTTSCoordinatorStateLocked()
	state := s.utterances[utteranceID]
	if state == nil || state.TTSOrder <= 0 || state.TTSSelected {
		return false
	}
	state.TTSSelected = true
	s.ttsReady[state.TTSOrder] = ttsDispatchTask{Sequence: state.TTSOrder, Skip: true}
	return true
}

func (s *interpreterSession) notifyTTSCoordinator() {
	if s.ttsNotify == nil {
		return
	}
	select {
	case s.ttsNotify <- struct{}{}:
	default:
	}
}

func (s *interpreterSession) runTTSCoordinator() {
	for {
		select {
		case <-s.ttsNotify:
			for {
				task, ok := s.nextAutoTTSTask()
				if !ok {
					break
				}
				if task.Skip {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				s.api.sendAutoTTS(ctx, task)
				cancel()
			}
		case <-s.ttsStop:
			return
		}
	}
}

func (s *interpreterSession) nextAutoTTSTask() (ttsDispatchTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTTSCoordinatorStateLocked()
	task, ok := s.ttsReady[s.nextTTSSend]
	if !ok {
		return ttsDispatchTask{}, false
	}
	delete(s.ttsReady, s.nextTTSSend)
	s.nextTTSSend++
	return task, true
}

func (s *interpreterSession) stopTTSCoordinator() {
	if s.ttsStop == nil {
		return
	}
	s.ttsStopOnce.Do(func() {
		close(s.ttsStop)
	})
}

func (s *interpreterSession) ensureTTSCoordinatorStateLocked() {
	if s.nextTTSSend <= 0 {
		s.nextTTSSend = 1
	}
	if s.ttsReady == nil {
		s.ttsReady = map[int]ttsDispatchTask{}
	}
}

func (s *interpreterSession) utterance(id string) *utteranceState {
	state := s.utterances[id]
	if state == nil {
		state = &utteranceState{}
		s.utterances[id] = state
	}
	return state
}

func (s *interpreterSession) appendSourceContext(entry contextEntry) {
	s.sourceContext = append(s.sourceContext, entry)
	if len(s.sourceContext) > contextWindowSize {
		s.sourceContext = s.sourceContext[len(s.sourceContext)-contextWindowSize:]
	}
}

func (api *API) writeTranslationResult(ctx context.Context, conn *wsConn, connectionID, userID string, result model.TranslationResult) {
	language := strings.TrimSpace(result.Language)
	if language == "" {
		if session := api.interpreter(connectionID); session != nil {
			language = session.language.TargetLanguage
		}
	}
	if language == "" {
		language = model.DefaultTargetLanguage
	}
	_ = writeLoggedWebSocketJSON(ctx, conn, connectionID, wsMessage{
		Type:         frontendTranslationType(result),
		ConnectionID: connectionID,
		CallID:       result.CallID,
		UserID:       userID,
		UtteranceID:  result.UtteranceID,
		SourceText:   result.SourceText,
		Text:         result.Text,
		IsFinal:      result.IsFinal,
		Engine:       result.Engine,
		Revised:      result.Revised,
		Language:     language,
		Metadata: map[string]string{
			"utteranceId": result.UtteranceID,
			"engine":      result.Engine,
		},
	})
}

func frontendTranslationType(result model.TranslationResult) string {
	switch strings.ToLower(strings.TrimSpace(result.Engine)) {
	case "tmt":
		if result.IsFinal {
			return "tmt_final"
		}
		return "tmt_result"
	case "deepseek-flash", "deepseek", "llm":
		return "llm_tmt_final"
	default:
		return "translation_result"
	}
}

func (api *API) sendAutoTTS(ctx context.Context, task ttsDispatchTask) {
	control := api.pbxControl()
	if control == nil {
		return
	}
	err := control.Send(ctx, pbxprotocol.Message{
		Type:         pbxprotocol.TypeTTSCommand,
		RequestID:    "auto-tts-" + task.UtteranceID,
		ConnectionID: task.ConnectionID,
		CallID:       task.CallID,
		UserID:       task.UserID,
		UtteranceID:  task.UtteranceID,
		Text:         task.Text,
		Voice:        task.Voice,
		Language:     task.Language,
		Metadata:     task.Metadata,
		Sequence:     task.Sequence,
	})
	if err != nil {
		slog.WarnContext(ctx, "发送自动 TTS 到 PBX 失败", slog.String("connectionId", task.ConnectionID), slog.String("callId", task.CallID), slog.String("utteranceId", task.UtteranceID), slog.Int("sequence", task.Sequence), slog.Any("error", err))
	}
}

func sourceContextLines(entries []contextEntry) []string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.SourceText)
		if text != "" {
			lines = append(lines, text)
		}
	}
	return lines
}

func normalizeComparable(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
