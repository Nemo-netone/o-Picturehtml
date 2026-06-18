// Package vocabulary consumes SQLite-backed vocabulary tasks.
package vocabulary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

type Store interface {
	ResetStaleVocabularyTasks(context.Context, time.Time, int) (int64, error)
	ClaimNextVocabularyTask(context.Context, string, int) (sessionstore.VocabularyTask, bool, error)
	SessionDetail(context.Context, string) (sessionstore.SessionDetail, error)
	CompleteVocabularyTask(context.Context, string, []sessionstore.VocabularyEntry, string, string, string) error
	FailVocabularyTask(context.Context, string, string, int) error
}

type LLMClient interface {
	ExtractVocabulary(context.Context, llm.VocabularyRequest) (llm.VocabularyResult, error)
}

type Options struct {
	WorkerID     string
	Concurrency  int
	PollInterval time.Duration
	LockTimeout  time.Duration
	MaxAttempts  int
}

type Consumer struct {
	store  Store
	llm    LLMClient
	opts   Options
	logger *slog.Logger
	active atomic.Int64
}

type inputSnapshot struct {
	Source string               `json:"source"`
	Reason string               `json:"reason,omitempty"`
	Texts  []llm.VocabularyText `json:"texts"`
}

func NewConsumer(store Store, llmClient LLMClient, opts Options, logger *slog.Logger) *Consumer {
	opts = normalizeOptions(opts)
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{store: store, llm: llmClient, opts: opts, logger: logger}
}

func (c *Consumer) Run(ctx context.Context) error {
	if c.store == nil {
		return errors.New("vocabulary worker store is nil")
	}
	if c.llm == nil {
		return errors.New("vocabulary worker llm client is nil")
	}
	var wg sync.WaitGroup
	for i := 0; i < c.opts.Concurrency; i++ {
		workerID := fmt.Sprintf("%s-%d", c.opts.WorkerID, i+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.runWorker(ctx, workerID)
		}()
	}
	wg.Wait()
	return nil
}

func (c *Consumer) runWorker(ctx context.Context, workerID string) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if _, err := c.store.ResetStaleVocabularyTasks(ctx, time.Now().Add(-c.opts.LockTimeout), c.opts.MaxAttempts); err != nil {
			c.logger.WarnContext(ctx, "恢复过期单词本任务失败", slog.String("workerId", workerID), slog.Any("error", err))
		}
		task, ok, err := c.store.ClaimNextVocabularyTask(ctx, workerID, c.opts.MaxAttempts)
		if err != nil {
			c.logger.WarnContext(ctx, "领取单词本任务失败", slog.String("workerId", workerID), slog.Any("error", err))
			c.sleep(ctx)
			continue
		}
		if !ok {
			c.sleep(ctx)
			continue
		}
		c.active.Add(1)
		func() {
			defer c.active.Add(-1)
			c.process(ctx, task)
		}()
	}
}

func (c *Consumer) ActiveTasks() int {
	if c == nil {
		return 0
	}
	active := c.active.Load()
	if active < 0 {
		return 0
	}
	return int(active)
}

func (c *Consumer) Concurrency() int {
	if c == nil || c.opts.Concurrency <= 0 {
		return 0
	}
	return c.opts.Concurrency
}

func (c *Consumer) process(ctx context.Context, task sessionstore.VocabularyTask) {
	detail, err := c.store.SessionDetail(ctx, task.SessionID)
	if err != nil {
		c.fail(ctx, task, err)
		return
	}
	input := buildVocabularyInput(detail)
	inputJSON, _ := json.Marshal(input)
	if len(input.Texts) == 0 {
		if input.Reason == "" {
			input.Reason = "no english text"
			inputJSON, _ = json.Marshal(input)
		}
		if err := c.store.CompleteVocabularyTask(ctx, task.ID, nil, string(inputJSON), "", `{"entries":[]}`); err != nil {
			c.logger.WarnContext(ctx, "提交空单词本任务失败", slog.String("taskId", task.ID), slog.Any("error", err))
		}
		return
	}

	result, err := c.llm.ExtractVocabulary(ctx, llm.VocabularyRequest{
		TaskID:    task.ID,
		SessionID: task.SessionID,
		TenantID:  task.TenantID,
		UserID:    task.UserID,
		Texts:     input.Texts,
		MaxWords:  task.MaxWords,
	})
	if err != nil {
		c.fail(ctx, task, err)
		return
	}
	entries := storeEntries(task.ID, result.Entries)
	if err := c.store.CompleteVocabularyTask(ctx, task.ID, entries, string(inputJSON), result.RawRequestJSON, result.RawResponseJSON); err != nil {
		c.logger.WarnContext(ctx, "提交单词本任务失败", slog.String("taskId", task.ID), slog.Any("error", err))
	}
}

func (c *Consumer) fail(ctx context.Context, task sessionstore.VocabularyTask, err error) {
	if failErr := c.store.FailVocabularyTask(ctx, task.ID, err.Error(), c.opts.MaxAttempts); failErr != nil {
		c.logger.WarnContext(ctx, "标记单词本任务失败异常", slog.String("taskId", task.ID), slog.Any("error", failErr))
	}
}

func (c *Consumer) sleep(ctx context.Context) {
	timer := time.NewTimer(c.opts.PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func buildVocabularyInput(detail sessionstore.SessionDetail) inputSnapshot {
	asrFinals := make([]sessionstore.ASRCallback, 0)
	hasEnglishASR := false
	for _, callback := range detail.ASRCallbacks {
		if callback.IsFinal != 1 {
			continue
		}
		asrFinals = append(asrFinals, callback)
		if isEnglishLanguage(callback.Language) {
			hasEnglishASR = true
		}
	}
	if hasEnglishASR {
		texts := make([]llm.VocabularyText, 0, len(asrFinals))
		for _, callback := range asrFinals {
			if !isEnglishLanguage(callback.Language) {
				continue
			}
			if text := strings.TrimSpace(callback.Text); text != "" {
				texts = append(texts, llm.VocabularyText{UtteranceID: callback.UtteranceID, Text: text})
			}
		}
		return inputSnapshot{Source: "asr_final", Texts: texts}
	}

	llmByASR := latestLLMEnglishByASR(detail.LLMRevisions)
	tmtByASR := latestTMTEnglishByASR(detail.MTTranslations)
	texts := make([]llm.VocabularyText, 0, len(asrFinals))
	for _, callback := range asrFinals {
		text := strings.TrimSpace(llmByASR[callback.ID])
		if text == "" {
			text = strings.TrimSpace(tmtByASR[callback.ID])
		}
		if text != "" {
			texts = append(texts, llm.VocabularyText{UtteranceID: callback.UtteranceID, Text: text})
		}
	}
	if len(texts) == 0 {
		return inputSnapshot{Source: "none", Reason: "no english text"}
	}
	return inputSnapshot{Source: "target_translation", Texts: texts}
}

func latestLLMEnglishByASR(records []sessionstore.LLMRevisionRecord) map[int64]string {
	out := map[int64]string{}
	for _, record := range records {
		if record.Status != "" && record.Status != "ok" {
			continue
		}
		if !llmTargetEnglish(record.MetadataJSON) {
			continue
		}
		text := strings.TrimSpace(record.RevisedText)
		if text == "" {
			continue
		}
		out[record.ASRCallbackID] = text
	}
	return out
}

func latestTMTEnglishByASR(records []sessionstore.MTTranslationRecord) map[int64]string {
	out := map[int64]string{}
	for _, record := range records {
		if record.Status != "" && record.Status != "ok" {
			continue
		}
		if record.IsFinal != 1 || !isEnglishLanguage(record.TargetLang) {
			continue
		}
		text := strings.TrimSpace(record.TargetText)
		if text != "" {
			out[record.ASRCallbackID] = text
		}
	}
	return out
}

func llmTargetEnglish(raw string) bool {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &metadata); err != nil {
		return false
	}
	value, _ := metadata["targetLanguage"].(string)
	return isEnglishLanguage(value)
}

func storeEntries(taskID string, entries []llm.VocabularyEntry) []sessionstore.VocabularyEntry {
	out := make([]sessionstore.VocabularyEntry, 0, len(entries))
	for i, entry := range entries {
		sourceIDs, _ := json.Marshal(entry.SourceUtteranceIDs)
		out = append(out, sessionstore.VocabularyEntry{
			TaskID:                 taskID,
			Ordinal:                i + 1,
			Word:                   entry.Word,
			Lemma:                  entry.Lemma,
			Phonetic:               entry.Phonetic,
			PartOfSpeech:           entry.PartOfSpeech,
			MeaningZH:              entry.MeaningZH,
			ExampleEN:              entry.ExampleEN,
			ExampleZH:              entry.ExampleZH,
			Occurrences:            entry.Occurrences,
			Difficulty:             entry.Difficulty,
			SourceUtteranceIDsJSON: string(sourceIDs),
		})
	}
	return out
}

func isEnglishLanguage(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "en" || strings.HasPrefix(normalized, "en-") || normalized == "16k_en" || normalized == "english"
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.WorkerID) == "" {
		opts.WorkerID = "vocabulary-worker"
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 5 * time.Minute
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	return opts
}
