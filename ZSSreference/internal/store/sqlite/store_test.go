// SQLite持久化层(GORM)：同传会话+ASR回调+机器翻译+LLM纠错记录
package sqlite_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	sessiondb "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestMigrateCreatesFiveTablesAndIndexesWithoutForeignKeys(t *testing.T) {
	store := openMigratedStore(t)
	db := store.DB()

	tables := sqliteNames(t, db.Raw(`
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`))
	expectedTables := []string{
		"asr_callbacks",
		"interpret_sessions",
		"llm_revision_records",
		"mt_translation_records",
		"providers",
		"vocabulary_entries",
		"vocabulary_tasks",
	}
	if !reflect.DeepEqual(tables, expectedTables) {
		t.Fatalf("unexpected tables:\nwant %#v\ngot  %#v", expectedTables, tables)
	}

	indexes := sqliteNames(t, db.Raw(`
		SELECT name
		FROM sqlite_master
		WHERE type = 'index' AND name LIKE 'idx_%'
		ORDER BY name
	`))
	expectedIndexes := []string{
		"idx_asr_provider",
		"idx_asr_session_final",
		"idx_asr_session_utterance",
		"idx_llm_asr",
		"idx_llm_provider_status",
		"idx_mt_asr",
		"idx_mt_provider_status",
		"idx_providers_capability_enabled",
		"idx_sessions_tenant_started",
		"idx_vocab_entries_task_ordinal",
		"idx_vocab_tasks_partition_status_created",
		"idx_vocab_tasks_session_created",
		"idx_vocab_tasks_status_created",
	}
	if !reflect.DeepEqual(indexes, expectedIndexes) {
		t.Fatalf("unexpected indexes:\nwant %#v\ngot  %#v", expectedIndexes, indexes)
	}

	for _, table := range expectedTables {
		var refs []struct {
			ID int `gorm:"column:id"`
		}
		if err := db.Raw("PRAGMA foreign_key_list(" + table + ")").Scan(&refs).Error; err != nil {
			t.Fatalf("inspect foreign keys for %s: %v", table, err)
		}
		if len(refs) != 0 {
			t.Fatalf("table %s must not declare database-level relationships: %#v", table, refs)
		}
	}

	mtColumns := tableColumns(t, db, "mt_translation_records")
	llmColumns := tableColumns(t, db, "llm_revision_records")
	for _, columns := range []map[string]bool{mtColumns, llmColumns} {
		if !columns["asr_callback_id"] {
			t.Fatalf("mt/llm records must include asr_callback_id")
		}
		for _, unexpected := range []string{"session_id", "call_id", "utterance_id"} {
			if columns[unexpected] {
				t.Fatalf("mt/llm records must not duplicate %s", unexpected)
			}
		}
	}
}

func TestStoreRecordsSessionProvidersAndASRLinkedOutputs(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	db := store.DB()

	asrProviderID, err := store.UpsertProvider(ctx, sessiondb.Provider{
		Name:         "tencent-asr",
		Capability:   "asr",
		Vendor:       "tencent",
		EndpointURL:  "wss://asr.cloud.tencent.com/asr/v2",
		APIKeyRef:    "env:SIMULSPEAK_TENCENT_ASR_SECRETKEY",
		Enabled:      1,
		IsDefault:    1,
		ConfigJSON:   `{"language":"en"}`,
		MetadataJSON: `{"appId":"1250000001"}`,
	})
	if err != nil {
		t.Fatalf("upsert asr provider: %v", err)
	}
	mtProviderID, err := store.UpsertProvider(ctx, sessiondb.Provider{
		Name:         "tencent-tmt",
		Capability:   "mt",
		Vendor:       "tencent",
		EndpointURL:  "https://tmt.tencentcloudapi.com",
		APIKeyRef:    "env:SIMULSPEAK_TENCENT_TMT_SECRETKEY",
		Enabled:      1,
		IsDefault:    1,
		ConfigJSON:   `{"source":"en","target":"zh"}`,
		MetadataJSON: `{"region":"ap-guangzhou","projectId":"0"}`,
	})
	if err != nil {
		t.Fatalf("upsert mt provider: %v", err)
	}
	llmProviderID, err := store.UpsertProvider(ctx, sessiondb.Provider{
		Name:         "openai-compatible",
		Capability:   "llm",
		Vendor:       "deepseek",
		EndpointURL:  "https://api.deepseek.com",
		Model:        "deepseek-chat",
		APIKeyRef:    "env:SIMULSPEAK_LLM_API_KEY",
		Enabled:      1,
		ConfigJSON:   `{"temperature":0.2}`,
		MetadataJSON: `{"promptVersion":"v1"}`,
	})
	if err != nil {
		t.Fatalf("upsert llm provider: %v", err)
	}

	err = store.CreateSession(ctx, sessiondb.InterpretSession{
		ID:                "call-001",
		TenantID:          "tenant-a",
		ConnectionID:      "session-001",
		UserID:            "user-001",
		TranslateStrategy: "hybrid",
		DubbingEnabled:    1,
		MetadataJSON:      `{"client":"pbx-probe"}`,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := store.AddProviderToSession(ctx, "call-001", "asr", asrProviderID); err != nil {
		t.Fatalf("add asr provider to session: %v", err)
	}
	if err := store.AddProviderToSession(ctx, "call-001", "mt", mtProviderID); err != nil {
		t.Fatalf("add mt provider to session: %v", err)
	}
	if err := store.AddProviderToSession(ctx, "call-001", "llm", llmProviderID); err != nil {
		t.Fatalf("add llm provider to session: %v", err)
	}
	if err := store.AddProviderToSession(ctx, "call-001", "mt", mtProviderID); err != nil {
		t.Fatalf("add duplicate mt provider to session: %v", err)
	}

	session, err := store.Session(ctx, "call-001")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	providerIDs := decodeProviderIDs(t, session.ProviderIDsJSON)
	assertIDs(t, providerIDs["asr"], []int64{asrProviderID})
	assertIDs(t, providerIDs["mt"], []int64{mtProviderID})
	assertIDs(t, providerIDs["llm"], []int64{llmProviderID})

	asrID, err := store.InsertASRCallback(ctx, sessiondb.ASRCallback{
		SessionID:    "call-001",
		ProviderID:   asrProviderID,
		CallID:       "call-001",
		UtteranceID:  "utt-001",
		SequenceNo:   1,
		Language:     "en",
		Text:         "hello world",
		IsFinal:      1,
		Confidence:   0.92,
		StartMS:      120,
		EndMS:        920,
		MetadataJSON: `{"requestId":"asr-request-001"}`,
		RawJSON:      `{"text":"hello world","final":true}`,
	})
	if err != nil {
		t.Fatalf("insert asr callback: %v", err)
	}

	mtID, err := store.InsertMTTranslation(ctx, sessiondb.MTTranslationRecord{
		ASRCallbackID:   asrID,
		ProviderID:      mtProviderID,
		ASRPhase:        "final",
		SourceLang:      "en",
		TargetLang:      "zh",
		SourceText:      "hello world",
		TargetText:      "你好，世界",
		IsFinal:         1,
		Status:          "ok",
		LatencyMS:       83,
		MetadataJSON:    `{"requestId":"tmt-request-001"}`,
		RawRequestJSON:  `{"SourceText":"hello world"}`,
		RawResponseJSON: `{"TargetText":"你好，世界"}`,
	})
	if err != nil {
		t.Fatalf("insert mt translation: %v", err)
	}

	llmID, err := store.InsertLLMRevision(ctx, sessiondb.LLMRevisionRecord{
		ASRCallbackID:    asrID,
		ProviderID:       llmProviderID,
		SourceText:       "hello world",
		DraftTranslation: "你好，世界",
		RevisedText:      "你好，世界",
		Revised:          0,
		Status:           "ok",
		LatencyMS:        211,
		ContextJSON:      `[{"source":"good morning","target":"早上好"}]`,
		TermsJSON:        `[{"source":"PBX","target":"PBX"}]`,
		MetadataJSON:     `{"requestId":"llm-request-001"}`,
		RawRequestJSON:   `{"model":"deepseek-chat"}`,
		RawResponseJSON:  `{"content":"你好，世界"}`,
	})
	if err != nil {
		t.Fatalf("insert llm revision: %v", err)
	}

	var mt sessiondb.MTTranslationRecord
	if err := db.First(&mt, mtID).Error; err != nil {
		t.Fatalf("load mt translation: %v", err)
	}
	if mt.ASRCallbackID != asrID || mt.TargetText != "你好，世界" || !strings.Contains(mt.MetadataJSON, "tmt-request-001") {
		t.Fatalf("unexpected mt record: %#v", mt)
	}

	var llm sessiondb.LLMRevisionRecord
	if err := db.First(&llm, llmID).Error; err != nil {
		t.Fatalf("load llm revision: %v", err)
	}
	if llm.ASRCallbackID != asrID || llm.SourceText != "hello world" || !strings.Contains(llm.MetadataJSON, "llm-request-001") {
		t.Fatalf("unexpected llm record: %#v", llm)
	}

	if err := db.Exec("DELETE FROM interpret_sessions WHERE id = ?", "call-001").Error; err != nil {
		t.Fatalf("delete session: %v", err)
	}
	var asrCount, mtCount, llmCount int64
	if err := db.Model(&sessiondb.ASRCallback{}).Where("session_id = ?", "call-001").Count(&asrCount).Error; err != nil {
		t.Fatalf("count asr callbacks: %v", err)
	}
	if err := db.Model(&sessiondb.MTTranslationRecord{}).Where("asr_callback_id = ?", asrID).Count(&mtCount).Error; err != nil {
		t.Fatalf("count mt translations: %v", err)
	}
	if err := db.Model(&sessiondb.LLMRevisionRecord{}).Where("asr_callback_id = ?", asrID).Count(&llmCount).Error; err != nil {
		t.Fatalf("count llm revisions: %v", err)
	}
	if asrCount != 1 || mtCount != 1 || llmCount != 1 {
		t.Fatalf("records should remain after deleting session because relationships are business-side only: asr=%d mt=%d llm=%d", asrCount, mtCount, llmCount)
	}
}

func TestStoreEndSessionMarksEndedIdempotently(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	if err := store.CreateSession(ctx, sessiondb.InterpretSession{
		ID:       "call-end",
		TenantID: "tenant-a",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EndSession(ctx, "call-end"); err != nil {
		t.Fatalf("end session: %v", err)
	}
	ended, err := store.Session(ctx, "call-end")
	if err != nil {
		t.Fatalf("load ended session: %v", err)
	}
	if ended.State != "ended" || ended.MediaState != "ended" || ended.EndedAt == "" {
		t.Fatalf("unexpected ended session: %#v", ended)
	}
	firstEndedAt := ended.EndedAt
	if err := store.EndSession(ctx, "call-end"); err != nil {
		t.Fatalf("end session again: %v", err)
	}
	endedAgain, err := store.Session(ctx, "call-end")
	if err != nil {
		t.Fatalf("load ended session again: %v", err)
	}
	if endedAgain.EndedAt != firstEndedAt {
		t.Fatalf("EndSession should preserve first ended_at: first=%s second=%s", firstEndedAt, endedAgain.EndedAt)
	}
}

func TestStoreListSessionsFiltersAndPaginates(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	sessions := []sessiondb.InterpretSession{
		{ID: "call-new", TenantID: "tenant-a", State: "active", StartedAt: "2026-06-06T03:00:00Z"},
		{ID: "call-old", TenantID: "tenant-a", State: "ended", StartedAt: "2026-06-06T02:00:00Z"},
		{ID: "call-other", TenantID: "tenant-b", State: "active", StartedAt: "2026-06-06T01:00:00Z"},
	}
	for _, session := range sessions {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create session %s: %v", session.ID, err)
		}
	}

	firstPage, err := store.ListSessions(ctx, sessiondb.SessionListQuery{TenantID: "tenant-a", Limit: 1})
	if err != nil {
		t.Fatalf("list tenant sessions: %v", err)
	}
	if firstPage.Total != 2 || firstPage.Limit != 1 || firstPage.Offset != 0 || len(firstPage.Sessions) != 1 || firstPage.Sessions[0].ID != "call-new" {
		t.Fatalf("unexpected first page: %#v", firstPage)
	}

	secondPage, err := store.ListSessions(ctx, sessiondb.SessionListQuery{TenantID: "tenant-a", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if secondPage.Total != 2 || len(secondPage.Sessions) != 1 || secondPage.Sessions[0].ID != "call-old" {
		t.Fatalf("unexpected second page: %#v", secondPage)
	}

	ended, err := store.ListSessions(ctx, sessiondb.SessionListQuery{TenantID: "tenant-a", State: "ended"})
	if err != nil {
		t.Fatalf("list ended sessions: %v", err)
	}
	if ended.Total != 1 || len(ended.Sessions) != 1 || ended.Sessions[0].ID != "call-old" {
		t.Fatalf("unexpected ended sessions: %#v", ended)
	}
}

func TestStoreSessionDetailLoadsASRAndLinkedOutputs(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	if err := store.CreateSession(ctx, sessiondb.InterpretSession{
		ID:                "call-detail",
		TenantID:          "tenant-a",
		ConnectionID:      "conn-detail",
		UserID:            "user-detail",
		TranslateStrategy: "hybrid",
		DubbingEnabled:    1,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.InsertASRCallback(ctx, sessiondb.ASRCallback{
		SessionID:   "call-detail",
		CallID:      "call-detail",
		UtteranceID: "utt-1",
		SequenceNo:  1,
		Language:    "en",
		Text:        "hello",
		IsFinal:     0,
	}); err != nil {
		t.Fatalf("insert partial asr: %v", err)
	}
	finalID, err := store.InsertASRCallback(ctx, sessiondb.ASRCallback{
		SessionID:    "call-detail",
		CallID:       "call-detail",
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
	if _, err := store.InsertASRCallback(ctx, sessiondb.ASRCallback{
		SessionID:   "call-detail",
		CallID:      "call-detail",
		UtteranceID: "utt-2",
		SequenceNo:  3,
		Language:    "en",
		Text:        "second sentence",
		IsFinal:     1,
	}); err != nil {
		t.Fatalf("insert second asr: %v", err)
	}
	if _, err := store.InsertMTTranslation(ctx, sessiondb.MTTranslationRecord{
		ASRCallbackID: finalID,
		ASRPhase:      "final",
		SourceText:    "hello world",
		TargetText:    "你好世界",
		IsFinal:       1,
	}); err != nil {
		t.Fatalf("insert mt translation: %v", err)
	}
	if _, err := store.InsertLLMRevision(ctx, sessiondb.LLMRevisionRecord{
		ASRCallbackID: finalID,
		SourceText:    "hello world",
		RevisedText:   "你好，世界",
		Revised:       1,
		ContextJSON:   `[{"sourceText":"previous sentence"}]`,
	}); err != nil {
		t.Fatalf("insert llm revision: %v", err)
	}

	detail, err := store.SessionDetail(ctx, "call-detail")
	if err != nil {
		t.Fatalf("load session detail: %v", err)
	}
	if detail.Session.ID != "call-detail" || len(detail.ASRCallbacks) != 2 || len(detail.MTTranslations) != 1 || len(detail.LLMRevisions) != 1 {
		t.Fatalf("unexpected detail: %#v", detail)
	}
	if detail.ASRCallbacks[0].ID != finalID || !allASRCallbacksFinal(detail.ASRCallbacks) {
		t.Fatalf("session detail should include only final asr callbacks ordered by sequence then id: %#v", detail.ASRCallbacks)
	}
	if detail.MTTranslations[0].ASRCallbackID != finalID || detail.LLMRevisions[0].ASRCallbackID != finalID {
		t.Fatalf("outputs should be linked to final asr callback: mt=%#v llm=%#v", detail.MTTranslations, detail.LLMRevisions)
	}
}

func allASRCallbacksFinal(callbacks []sessiondb.ASRCallback) bool {
	for _, callback := range callbacks {
		if callback.IsFinal != 1 {
			return false
		}
	}
	return true
}

func TestVocabularyTasksCancelSupersededPendingAndPartitionByUser(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	if err := store.CreateSession(ctx, sessiondb.InterpretSession{ID: "call-vocab", TenantID: "tenant-a", UserID: "user-1"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	first, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-1", SessionID: "call-vocab", MaxWords: 10})
	if err != nil {
		t.Fatalf("create first vocabulary task: %v", err)
	}
	if first.PartitionKey != "tenant-a:user-1" || first.MaxWords != 10 || first.Status != sessiondb.VocabularyTaskStatusPending {
		t.Fatalf("unexpected first task: %#v", first)
	}
	second, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-2", SessionID: "call-vocab"})
	if err != nil {
		t.Fatalf("create second vocabulary task: %v", err)
	}
	if second.PartitionKey != "tenant-a:user-1" || second.MaxWords != sessiondb.DefaultVocabularyMaxWords {
		t.Fatalf("unexpected second task: %#v", second)
	}
	firstDetail, err := store.VocabularyTaskDetail(ctx, "task-1")
	if err != nil {
		t.Fatalf("load first task: %v", err)
	}
	if firstDetail.Task.Status != sessiondb.VocabularyTaskStatusCancelled {
		t.Fatalf("first pending task should be cancelled, got %#v", firstDetail.Task)
	}
}

func TestVocabularyClaimPreservesUserOrderAndAllowsDifferentUsers(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	for _, session := range []sessiondb.InterpretSession{
		{ID: "call-a1", TenantID: "tenant-a", UserID: "user-a"},
		{ID: "call-a2", TenantID: "tenant-a", UserID: "user-a"},
		{ID: "call-b1", TenantID: "tenant-a", UserID: "user-b"},
	} {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create session %s: %v", session.ID, err)
		}
	}
	if _, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-a1", SessionID: "call-a1", CreatedAt: "2026-06-07T01:00:00Z"}); err != nil {
		t.Fatalf("create task-a1: %v", err)
	}
	if _, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-a2", SessionID: "call-a2", CreatedAt: "2026-06-07T01:01:00Z"}); err != nil {
		t.Fatalf("create task-a2: %v", err)
	}
	if _, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-b1", SessionID: "call-b1", CreatedAt: "2026-06-07T01:02:00Z"}); err != nil {
		t.Fatalf("create task-b1: %v", err)
	}

	claimedA, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-1", 3)
	if err != nil || !ok || claimedA.ID != "task-a1" {
		t.Fatalf("claim first task: task=%#v ok=%v err=%v", claimedA, ok, err)
	}
	claimedB, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-2", 3)
	if err != nil || !ok || claimedB.ID != "task-b1" {
		t.Fatalf("different user should claim while user-a is running: task=%#v ok=%v err=%v", claimedB, ok, err)
	}
	claimedNone, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-3", 3)
	if err != nil || ok || claimedNone.ID != "" {
		t.Fatalf("later task in same user partition must wait: task=%#v ok=%v err=%v", claimedNone, ok, err)
	}
	if err := store.CompleteVocabularyTask(ctx, "task-a1", nil, "{}", "{}", `{"entries":[]}`); err != nil {
		t.Fatalf("complete task-a1: %v", err)
	}
	claimedA2, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-3", 3)
	if err != nil || !ok || claimedA2.ID != "task-a2" {
		t.Fatalf("claim second user-a task after commit: task=%#v ok=%v err=%v", claimedA2, ok, err)
	}
}

func TestVocabularyClaimConcurrentDoesNotReturnBusy(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	for i, session := range []sessiondb.InterpretSession{
		{ID: "call-concurrent-a", TenantID: "tenant-a", UserID: "user-a"},
		{ID: "call-concurrent-b", TenantID: "tenant-a", UserID: "user-b"},
		{ID: "call-concurrent-c", TenantID: "tenant-a", UserID: "user-c"},
		{ID: "call-concurrent-d", TenantID: "tenant-a", UserID: "user-d"},
	} {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create session %s: %v", session.ID, err)
		}
		if _, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{
			ID:        "task-concurrent-" + session.UserID,
			SessionID: session.ID,
			CreatedAt: time.Date(2026, 6, 7, 1, i, 0, 0, time.UTC).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("create task for %s: %v", session.ID, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < cap(errCh); i++ {
		workerID := "worker-concurrent-" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.ClaimNextVocabularyTask(ctx, workerID, 3)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent claim should not fail: %v", err)
		}
	}
}

func TestVocabularyResetStaleRunning(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)

	if err := store.CreateSession(ctx, sessiondb.InterpretSession{ID: "call-stale", TenantID: "tenant-a", UserID: "user-stale"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.CreateVocabularyTask(ctx, sessiondb.VocabularyTask{ID: "task-stale", SessionID: "call-stale"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-1", 3)
	if err != nil || !ok || claimed.ID != "task-stale" {
		t.Fatalf("claim task: task=%#v ok=%v err=%v", claimed, ok, err)
	}
	if err := store.DB().Model(&sessiondb.VocabularyTask{}).
		Where("id = ?", "task-stale").
		Updates(map[string]any{"locked_at": "2026-06-07T01:00:00Z"}).Error; err != nil {
		t.Fatalf("age lock: %v", err)
	}
	n, err := store.ResetStaleVocabularyTasks(ctx, mustParseTime(t, "2026-06-07T01:01:00Z"), 3)
	if err != nil {
		t.Fatalf("reset stale: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one stale task reset, got %d", n)
	}
	detail, err := store.VocabularyTaskDetail(ctx, "task-stale")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if detail.Task.Status != sessiondb.VocabularyTaskStatusPending {
		t.Fatalf("expected pending after stale reset, got %#v", detail.Task)
	}
}

func TestEnsureInitializedDetectsAndMigratesMissingSchema(t *testing.T) {
	ctx := context.Background()
	store, err := sessiondb.Open(t.TempDir() + "/simulspeak.db")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	})

	status, err := store.InitializationStatus(ctx)
	if err != nil {
		t.Fatalf("inspect empty sqlite store: %v", err)
	}
	if status.Initialized {
		t.Fatalf("new sqlite store should not be initialized before EnsureInitialized")
	}
	if len(status.MissingTables) != len(sessiondb.RequiredTables) {
		t.Fatalf("unexpected missing tables: %#v", status.MissingTables)
	}
	if len(status.MissingIndexes) != len(sessiondb.RequiredIndexes) {
		t.Fatalf("unexpected missing indexes: %#v", status.MissingIndexes)
	}

	result, err := store.EnsureInitialized(ctx)
	if err != nil {
		t.Fatalf("ensure sqlite store initialized: %v", err)
	}
	if result.AlreadyInitialized || !result.Migrated {
		t.Fatalf("first initialization should migrate missing schema: %#v", result)
	}

	status, err = store.InitializationStatus(ctx)
	if err != nil {
		t.Fatalf("inspect migrated sqlite store: %v", err)
	}
	if !status.Initialized {
		t.Fatalf("sqlite store should be initialized after migration: %#v", status)
	}

	result, err = store.EnsureInitialized(ctx)
	if err != nil {
		t.Fatalf("ensure sqlite store initialized again: %v", err)
	}
	if !result.AlreadyInitialized || result.Migrated {
		t.Fatalf("second initialization should be a no-op: %#v", result)
	}
}

func openMigratedStore(t *testing.T) *sessiondb.Store {
	t.Helper()

	store, err := sessiondb.Open(t.TempDir() + "/simulspeak.db")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate sqlite store: %v", err)
	}
	return store
}

func sqliteNames(t *testing.T, tx *gorm.DB) []string {
	t.Helper()

	var rows []struct {
		Name string `gorm:"column:name"`
	}
	if err := tx.Scan(&rows).Error; err != nil {
		t.Fatalf("scan sqlite names: %v", err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func tableColumns(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()

	var rows []struct {
		Name string `gorm:"column:name"`
	}
	if err := db.Raw("PRAGMA table_info(" + table + ")").Scan(&rows).Error; err != nil {
		t.Fatalf("inspect columns for %s: %v", table, err)
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.Name] = true
	}
	return out
}

func decodeProviderIDs(t *testing.T, raw string) map[string][]int64 {
	t.Helper()

	var providerIDs map[string][]int64
	if err := json.Unmarshal([]byte(raw), &providerIDs); err != nil {
		t.Fatalf("decode provider ids json: %v", err)
	}
	return providerIDs
}

func assertIDs(t *testing.T, got, want []int64) {
	t.Helper()

	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected provider ids: want %#v got %#v", want, got)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %s: %v", value, err)
	}
	return parsed
}
