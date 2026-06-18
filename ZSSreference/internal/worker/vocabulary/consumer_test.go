package vocabulary

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

type fakeVocabularyLLM struct {
	texts []llm.VocabularyText
}

func (f *fakeVocabularyLLM) ExtractVocabulary(ctx context.Context, req llm.VocabularyRequest) (llm.VocabularyResult, error) {
	f.texts = append([]llm.VocabularyText(nil), req.Texts...)
	return llm.VocabularyResult{
		Entries: []llm.VocabularyEntry{{
			Word:               "architecture",
			Lemma:              "architecture",
			MeaningZH:          "架构；体系结构",
			ExampleEN:          req.Texts[0].Text,
			ExampleZH:          "我们使用架构。",
			Occurrences:        1,
			Difficulty:         "B2",
			SourceUtteranceIDs: []string{req.Texts[0].UtteranceID},
		}},
		RawRequestJSON:  `{"test":true}`,
		RawResponseJSON: `{"entries":[{"word":"architecture"}]}`,
	}, nil
}

type blockingVocabularyLLM struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *blockingVocabularyLLM) ExtractVocabulary(ctx context.Context, req llm.VocabularyRequest) (llm.VocabularyResult, error) {
	f.once.Do(func() {
		close(f.started)
	})
	select {
	case <-ctx.Done():
		return llm.VocabularyResult{}, ctx.Err()
	case <-f.release:
		return llm.VocabularyResult{
			Entries: []llm.VocabularyEntry{{
				Word:               "architecture",
				MeaningZH:          "架构",
				ExampleEN:          req.Texts[0].Text,
				SourceUtteranceIDs: []string{req.Texts[0].UtteranceID},
			}},
			RawRequestJSON:  `{"test":true}`,
			RawResponseJSON: `{"entries":[{"word":"architecture"}]}`,
		}, nil
	}
}

func TestConsumerProcessUsesEnglishASRFinal(t *testing.T) {
	ctx := context.Background()
	store := openWorkerStore(t)
	seedVocabularySession(t, store, "call-asr-en", "tenant-a", "user-a")
	if _, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:   "call-asr-en",
		CallID:      "call-asr-en",
		UtteranceID: "utt-en",
		SequenceNo:  1,
		Language:    "en",
		Text:        "We use a transformer architecture.",
		IsFinal:     1,
	}); err != nil {
		t.Fatalf("insert asr: %v", err)
	}
	task, err := store.CreateVocabularyTask(ctx, sessionstore.VocabularyTask{ID: "task-asr-en", SessionID: "call-asr-en"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-1", 3)
	if err != nil || !ok || claimed.ID != task.ID {
		t.Fatalf("claim task: task=%#v ok=%v err=%v", claimed, ok, err)
	}
	fake := &fakeVocabularyLLM{}
	consumer := NewConsumer(store, fake, Options{}, nil)
	consumer.process(ctx, claimed)

	if len(fake.texts) != 1 || !strings.Contains(fake.texts[0].Text, "transformer architecture") {
		t.Fatalf("unexpected llm input: %#v", fake.texts)
	}
	detail, err := store.VocabularyTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("load task detail: %v", err)
	}
	if detail.Task.Status != sessionstore.VocabularyTaskStatusSucceeded || len(detail.Entries) != 1 || detail.Entries[0].Word != "architecture" {
		t.Fatalf("unexpected task detail: %#v", detail)
	}
}

func TestConsumerProcessUsesEnglishTargetTranslation(t *testing.T) {
	ctx := context.Background()
	store := openWorkerStore(t)
	seedVocabularySession(t, store, "call-target-en", "tenant-a", "user-a")
	asrID, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:   "call-target-en",
		CallID:      "call-target-en",
		UtteranceID: "utt-zh",
		SequenceNo:  1,
		Language:    "zh",
		Text:        "我们使用架构。",
		IsFinal:     1,
	})
	if err != nil {
		t.Fatalf("insert asr: %v", err)
	}
	if _, err := store.InsertLLMRevision(ctx, sessionstore.LLMRevisionRecord{
		ASRCallbackID: asrID,
		SourceText:    "我们使用架构。",
		RevisedText:   "We use an architecture.",
		Status:        "ok",
		MetadataJSON:  `{"targetLanguage":"en"}`,
	}); err != nil {
		t.Fatalf("insert llm revision: %v", err)
	}
	task, err := store.CreateVocabularyTask(ctx, sessionstore.VocabularyTask{ID: "task-target-en", SessionID: "call-target-en"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-1", 3)
	if err != nil || !ok || claimed.ID != task.ID {
		t.Fatalf("claim task: task=%#v ok=%v err=%v", claimed, ok, err)
	}
	fake := &fakeVocabularyLLM{}
	consumer := NewConsumer(store, fake, Options{}, nil)
	consumer.process(ctx, claimed)

	if len(fake.texts) != 1 || fake.texts[0].Text != "We use an architecture." {
		t.Fatalf("unexpected llm input: %#v", fake.texts)
	}
}

func TestConsumerProcessCompletesEmptyWhenNoEnglishText(t *testing.T) {
	ctx := context.Background()
	store := openWorkerStore(t)
	seedVocabularySession(t, store, "call-no-en", "tenant-a", "user-a")
	if _, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:   "call-no-en",
		CallID:      "call-no-en",
		UtteranceID: "utt-zh",
		SequenceNo:  1,
		Language:    "zh",
		Text:        "我们使用架构。",
		IsFinal:     1,
	}); err != nil {
		t.Fatalf("insert asr: %v", err)
	}
	task, err := store.CreateVocabularyTask(ctx, sessionstore.VocabularyTask{ID: "task-no-en", SessionID: "call-no-en"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, ok, err := store.ClaimNextVocabularyTask(ctx, "worker-1", 3)
	if err != nil || !ok || claimed.ID != task.ID {
		t.Fatalf("claim task: task=%#v ok=%v err=%v", claimed, ok, err)
	}
	fake := &fakeVocabularyLLM{}
	consumer := NewConsumer(store, fake, Options{}, nil)
	consumer.process(ctx, claimed)

	detail, err := store.VocabularyTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("load detail: %v", err)
	}
	if detail.Task.Status != sessionstore.VocabularyTaskStatusSucceeded || len(detail.Entries) != 0 || len(fake.texts) != 0 {
		t.Fatalf("expected empty succeeded task, got %#v input=%#v", detail, fake.texts)
	}
}

func TestConsumerTracksActiveTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := openWorkerStore(t)
	seedVocabularySession(t, store, "call-active", "tenant-a", "user-a")
	if _, err := store.InsertASRCallback(ctx, sessionstore.ASRCallback{
		SessionID:   "call-active",
		CallID:      "call-active",
		UtteranceID: "utt-active",
		SequenceNo:  1,
		Language:    "en",
		Text:        "We use a transformer architecture.",
		IsFinal:     1,
	}); err != nil {
		t.Fatalf("insert asr: %v", err)
	}
	if _, err := store.CreateVocabularyTask(ctx, sessionstore.VocabularyTask{ID: "task-active", SessionID: "call-active"}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	fake := &blockingVocabularyLLM{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	consumer := NewConsumer(store, fake, Options{Concurrency: 1, PollInterval: 10 * time.Millisecond}, nil)
	done := make(chan error, 1)
	go func() {
		done <- consumer.Run(ctx)
	}()

	select {
	case <-fake.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for vocabulary task to start")
	}
	if active := consumer.ActiveTasks(); active != 1 {
		t.Fatalf("expected one active task while llm is blocked, got %d", active)
	}

	close(fake.release)
	waitForActiveTasks(t, consumer, 0)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("consumer stopped with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for consumer to stop")
	}
}

func openWorkerStore(t *testing.T) *sessionstore.Store {
	t.Helper()
	store, err := sessionstore.Open(":memory:")
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

func waitForActiveTasks(t *testing.T, consumer *Consumer, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if consumer.ActiveTasks() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for active task count %d, got %d", want, consumer.ActiveTasks())
}

func seedVocabularySession(t *testing.T, store *sessionstore.Store, id, tenantID, userID string) {
	t.Helper()
	if err := store.CreateSession(context.Background(), sessionstore.InterpretSession{
		ID:       id,
		TenantID: tenantID,
		UserID:   userID,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
}
