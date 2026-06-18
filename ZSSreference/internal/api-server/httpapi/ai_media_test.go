// HTTP API层：路由注册+WebSocket处理+同传解释器+PBX消息桥接+请求日志
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	llmpkg "github.com/SATA260/SimulSpeak1/internal/ai/llm"
	"github.com/SATA260/SimulSpeak1/internal/gateway"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

// TestProviderLogSummary_RedactsTencentValues 验证 provider 日志只显示必填项是否配置，不泄露实际密钥。
func TestProviderLogSummary_RedactsTencentValues(t *testing.T) {
	summary := providerLogSummary(wsMessage{ProviderConfigs: map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {
			Provider: "tencent-asr",
			Params:   map[string]string{"appId": "1250000000"},
			Secrets:  map[string]string{"secretId": "secret-id", "secretKey": "secret-key"},
		},
		model.CapabilityTypeTMT: {
			Provider: "tencent-tmt",
			Params:   map[string]string{"region": "ap-guangzhou", "projectId": "0"},
			Secrets:  map[string]string{"secretId": "tmt-secret-id", "secretKey": "tmt-secret-key"},
		},
	}})
	if len(summary) != 2 {
		t.Fatalf("unexpected provider summary: %#v", summary)
	}
	joined := strings.Join(summary, " ")
	if !strings.Contains(joined, "appId=true") || !strings.Contains(joined, "secretKey=true") || !strings.Contains(joined, "secretId=true") {
		t.Fatalf("unexpected provider summary: %#v", summary)
	}
	if strings.Contains(joined, "secret-key") || strings.Contains(joined, "tmt-secret-key") || strings.Contains(joined, "1250000000") {
		t.Fatalf("provider summary leaked values: %#v", summary)
	}
}

func TestWebSocketInterpreterOptions(t *testing.T) {
	conn := &wsConn{}

	ack, err := conn.setInterpreterOptions(map[string]string{
		"translateStrategy": "hybrid",
		"dubbing":           "1",
	})
	if err != nil {
		t.Fatalf("set interpreter options: %v", err)
	}
	if ack["requestedTranslateStrategy"] != "hybrid" || ack["translateStrategy"] != "hybrid" || ack["dubbing"] != "1" {
		t.Fatalf("unexpected interpreter metadata: %#v", ack)
	}
	if ack["sourceLanguage"] != "en" || ack["targetLanguage"] != "zh" || ack["asrEngineType"] != "16k_en" {
		t.Fatalf("unexpected language metadata: %#v", ack)
	}
	if !conn.dubbingEnabled() {
		t.Fatal("expected dubbing to be enabled")
	}

	strategy := conn.setStrategy("deepseek")
	if strategy != "deepseek" {
		t.Fatalf("strategy should keep ai mode, got %s", strategy)
	}

	if conn.setDubbing("0") {
		t.Fatal("expected dubbing to be disabled")
	}
}

func TestWebSocketInterpreterOptionsLanguageOverride(t *testing.T) {
	conn := &wsConn{}
	ack, err := conn.setInterpreterOptions(map[string]string{
		"sourceLanguage": "zh",
		"targetLanguage": "en",
		"ttsVoiceType":   "101050",
	})
	if err != nil {
		t.Fatalf("set interpreter options: %v", err)
	}
	if ack["sourceLanguage"] != "zh" || ack["targetLanguage"] != "en" || ack["asrEngineType"] != "16k_zh" || ack["ttsLanguage"] != "en-US" || ack["ttsPrimaryLanguage"] != "2" || ack["ttsVoiceType"] != "101050" {
		t.Fatalf("unexpected language metadata: %#v", ack)
	}
	options := conn.currentLanguageOptions()
	if options.SourceLanguage != "zh" || options.TargetLanguage != "en" || options.TTSPrimaryLanguage != 2 {
		t.Fatalf("unexpected stored options: %#v", options)
	}
}

func TestWebSocketInterpreterOptionsRejectsInvalidLanguage(t *testing.T) {
	conn := &wsConn{}
	if _, err := conn.setInterpreterOptions(map[string]string{"sourceLanguage": "en", "targetLanguage": "en"}); err == nil {
		t.Fatal("expected invalid same-language pair to fail")
	}
}

func TestInterpreterTMTFirstThenDeepSeekOverwriteAndAutoTTS(t *testing.T) {
	api, conn, client, pbx, llm, store := newInterpreterTestHarness(t, "深度译文", make(chan struct{}))
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", true)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "hello world",
			IsFinal:     true,
			Language:    "en",
			Confidence:  0.98,
		})
	})
	asr := readTestWSMessage(t, client)
	waitAsync(t, done)
	if asr.Type != "asr_result" || !asr.IsFinal || asr.Text != "hello world" {
		t.Fatalf("unexpected asr message: %#v", asr)
	}
	req := llm.nextRequest(t)
	if req.Text != "hello world" || len(req.SourceContext) != 0 || len(req.Context) != 0 || req.FastTranslation != "" {
		t.Fatalf("unexpected llm request: %#v", req)
	}

	done = runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "hello world",
			Text:        "机器译文",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	tmt := readTestWSMessage(t, client)
	waitAsync(t, done)
	if tmt.Type != "tmt_final" || tmt.Engine != "tmt" || tmt.Text != "机器译文" {
		t.Fatalf("unexpected tmt message: %#v", tmt)
	}
	tts := pbx.nextMessage(t)
	if tts.Type != pbxprotocol.TypeTTSCommand || tts.Text != "机器译文" || tts.UtteranceID != "utt-1" || tts.Sequence != 1 {
		t.Fatalf("expected first available translation to trigger ordered tts command, got %#v", tts)
	}

	llm.release()
	deepseek := readTestWSMessage(t, client)
	if deepseek.Type != "llm_tmt_final" || deepseek.Engine != "deepseek-flash" || deepseek.Text != "深度译文" || !deepseek.Revised {
		t.Fatalf("unexpected deepseek message: %#v", deepseek)
	}
	pbx.assertNoMessage(t)

	waitStoreCount(t, store, &sessionstore.ASRCallback{}, 1)
	waitStoreCount(t, store, &sessionstore.MTTranslationRecord{}, 1)
	waitStoreCount(t, store, &sessionstore.LLMRevisionRecord{}, 1)
	var mtRecord sessionstore.MTTranslationRecord
	if err := store.DB().Take(&mtRecord).Error; err != nil {
		t.Fatalf("load mt translation: %v", err)
	}
	if mtRecord.TargetText != "机器译文" {
		t.Fatalf("unexpected mt target text: %#v", mtRecord)
	}
	var llmRecord sessionstore.LLMRevisionRecord
	if err := store.DB().Take(&llmRecord).Error; err != nil {
		t.Fatalf("load llm revision: %v", err)
	}
	if llmRecord.DraftTranslation != "" || llmRecord.RevisedText != "深度译文" {
		t.Fatalf("llm record should not store tmt draft: %#v", llmRecord)
	}
}

func TestInterpreterTMTStrategyAutoTTSAfterTMTFinal(t *testing.T) {
	api, conn, client, pbx, llm, _ := newInterpreterTestHarness(t, "unused", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "tmt", true)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "hello world",
			IsFinal:     true,
			Language:    "en",
		})
	})
	asr := readTestWSMessage(t, client)
	waitAsync(t, done)
	if asr.Type != "asr_result" {
		t.Fatalf("unexpected asr message: %#v", asr)
	}
	llm.assertNoRequest(t)

	done = runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "hello world",
			Text:        "机器译文",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	tmt := readTestWSMessage(t, client)
	waitAsync(t, done)
	if tmt.Type != "tmt_final" || tmt.Text != "机器译文" {
		t.Fatalf("unexpected tmt message: %#v", tmt)
	}
	tts := pbx.nextMessage(t)
	if tts.Type != pbxprotocol.TypeTTSCommand || tts.Text != "机器译文" || tts.UtteranceID != "utt-1" || tts.Sequence != 1 {
		t.Fatalf("expected tmt auto tts command, got %#v", tts)
	}
}

func TestInterpreterUsesSessionLanguageForTMTLLMAndTTS(t *testing.T) {
	api, conn, client, pbx, llm, store := newInterpreterTestHarness(t, "english revision", make(chan struct{}))
	ctx := context.Background()
	language, err := model.NormalizeSessionLanguageOptions(map[string]string{
		"sourceLanguage": "zh",
		"targetLanguage": "en",
		"ttsVoiceType":   "101050",
	})
	if err != nil {
		t.Fatalf("normalize language: %v", err)
	}
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", true, language)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "你好世界",
			IsFinal:     true,
			Language:    "zh",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	req := llm.nextRequest(t)
	if req.SourceLang != "zh" || req.TargetLang != "en" {
		t.Fatalf("unexpected llm language direction: %#v", req)
	}

	done = runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "你好世界",
			Text:        "hello world",
			IsFinal:     true,
			Engine:      "tmt",
			Language:    "en",
		})
	})
	tmt := readTestWSMessage(t, client)
	waitAsync(t, done)
	if tmt.Language != "en" {
		t.Fatalf("expected tmt target language en, got %#v", tmt)
	}

	llm.release()
	deepseek := readTestWSMessage(t, client)
	if deepseek.Language != "en" || deepseek.Text != "english revision" {
		t.Fatalf("unexpected llm message: %#v", deepseek)
	}
	tts := pbx.nextMessage(t)
	if tts.Language != "en-US" || tts.Voice != "101050" || tts.Metadata["ttsPrimaryLanguage"] != "2" {
		t.Fatalf("unexpected tts language options: %#v", tts)
	}

	var mtRecord sessionstore.MTTranslationRecord
	if err := store.DB().Take(&mtRecord).Error; err != nil {
		t.Fatalf("load mt translation: %v", err)
	}
	if mtRecord.SourceLang != "zh" || mtRecord.TargetLang != "en" {
		t.Fatalf("unexpected mt record language direction: %#v", mtRecord)
	}
}

func TestInterpreterTMTStrategyDubbingDisabledSkipsAutoTTS(t *testing.T) {
	api, conn, client, pbx, _, _ := newInterpreterTestHarness(t, "unused", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "tmt", false)
	conn.setDubbing("0")

	done := runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "hello world",
			Text:        "机器译文",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	tmt := readTestWSMessage(t, client)
	waitAsync(t, done)
	if tmt.Type != "tmt_final" || tmt.Text != "机器译文" {
		t.Fatalf("unexpected tmt message: %#v", tmt)
	}
	pbx.assertNoMessage(t)
}

func TestInterpreterASRPartialDoesNotTriggerLLM(t *testing.T) {
	api, conn, client, _, llm, _ := newInterpreterTestHarness(t, "partial should not translate", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", false)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "hello",
			IsFinal:     false,
			Language:    "en",
		})
	})
	asr := readTestWSMessage(t, client)
	waitAsync(t, done)
	if asr.Type != "asr_result" || asr.IsFinal {
		t.Fatalf("unexpected asr message: %#v", asr)
	}
	llm.assertNoRequest(t)
	assertNoWSMessage(t, client)
}

func TestInterpreterLLMUsesPreviousASRFinalSourceContext(t *testing.T) {
	api, conn, client, _, llm, _ := newInterpreterTestHarness(t, "上下文译文", make(chan struct{}))
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", false)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "first sentence",
			IsFinal:     true,
			Language:    "en",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	firstReq := llm.nextRequest(t)
	if firstReq.Text != "first sentence" || len(firstReq.SourceContext) != 0 {
		t.Fatalf("unexpected first llm request: %#v", firstReq)
	}

	done = runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-2",
			Text:        "second sentence",
			IsFinal:     true,
			Language:    "en",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	secondReq := llm.nextRequest(t)
	if secondReq.Text != "second sentence" || len(secondReq.SourceContext) != 1 || secondReq.SourceContext[0] != "first sentence" {
		t.Fatalf("unexpected second llm request: %#v", secondReq)
	}

	llm.release()
	_ = readTestWSMessage(t, client)
	_ = readTestWSMessage(t, client)
}

func TestInterpreterLLMSourceContextWindowAndStoredJSON(t *testing.T) {
	api, conn, client, _, llm, store := newInterpreterTestHarness(t, "窗口译文", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", false)

	var lastReq llmpkg.Request
	for i := 1; i <= 8; i++ {
		utteranceID := "utt-" + string(rune('0'+i))
		text := "sentence " + string(rune('0'+i))
		done := runAsync(func() {
			api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
				CallID:      "call-1",
				UtteranceID: utteranceID,
				Text:        text,
				IsFinal:     true,
				Language:    "en",
			})
		})
		_ = readTestWSMessage(t, client)
		waitAsync(t, done)
		lastReq = llm.nextRequest(t)
		_ = readTestWSMessage(t, client)
	}

	want := []string{"sentence 2", "sentence 3", "sentence 4", "sentence 5", "sentence 6", "sentence 7"}
	if !stringSlicesEqual(lastReq.SourceContext, want) {
		t.Fatalf("unexpected source context window: got %#v want %#v", lastReq.SourceContext, want)
	}
	if strings.Contains(strings.Join(lastReq.SourceContext, "\n"), "sentence 8") {
		t.Fatalf("source context must not include current sentence: %#v", lastReq.SourceContext)
	}

	var revision sessionstore.LLMRevisionRecord
	waitUntil(t, func() bool {
		return store.DB().Where("source_text = ?", "sentence 8").Take(&revision).Error == nil
	})
	var stored []contextEntry
	if err := json.Unmarshal([]byte(revision.ContextJSON), &stored); err != nil {
		t.Fatalf("decode context_json: %v", err)
	}
	gotStored := sourceContextLines(stored)
	if !stringSlicesEqual(gotStored, want) {
		t.Fatalf("unexpected stored context: got %#v want %#v raw=%s", gotStored, want, revision.ContextJSON)
	}
}

func TestInterpreterDeepSeekFirstStillForwardsLateTMTWithoutSecondTTS(t *testing.T) {
	api, conn, client, pbx, llm, store := newInterpreterTestHarness(t, "AI 最终译文", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "deepseek", true)

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "good morning",
			IsFinal:     true,
			Language:    "en",
		})
	})
	asr := readTestWSMessage(t, client)
	waitAsync(t, done)
	if asr.Type != "asr_result" {
		t.Fatalf("unexpected first message: %#v", asr)
	}
	_ = llm.nextRequest(t)
	deepseek := readTestWSMessage(t, client)
	if deepseek.Type != "llm_tmt_final" || deepseek.Engine != "deepseek-flash" || deepseek.Text != "AI 最终译文" {
		t.Fatalf("unexpected deepseek message: %#v", deepseek)
	}
	tts := pbx.nextMessage(t)
	if tts.Text != "AI 最终译文" || tts.Sequence != 1 {
		t.Fatalf("unexpected deepseek tts message: %#v", tts)
	}

	done = runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "good morning",
			Text:        "迟到的机器译文",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	lateTMT := readTestWSMessage(t, client)
	waitAsync(t, done)
	if lateTMT.Type != "tmt_final" || lateTMT.Engine != "tmt" || lateTMT.Text != "迟到的机器译文" {
		t.Fatalf("unexpected late tmt message: %#v", lateTMT)
	}
	pbx.assertNoMessage(t)
	waitStoreCount(t, store, &sessionstore.MTTranslationRecord{}, 1)
}

func TestInterpreterAutoTTSSendsInASRFinalOrder(t *testing.T) {
	api, conn, client, pbx, llm, _ := newInterpreterTestHarness(t, "unused", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "tmt", true)

	for _, item := range []struct {
		utteranceID string
		text        string
	}{
		{utteranceID: "utt-1", text: "first sentence"},
		{utteranceID: "utt-2", text: "second sentence"},
	} {
		done := runAsync(func() {
			api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
				CallID:      "call-1",
				UtteranceID: item.utteranceID,
				Text:        item.text,
				IsFinal:     true,
				Language:    "en",
			})
		})
		_ = readTestWSMessage(t, client)
		waitAsync(t, done)
	}
	llm.assertNoRequest(t)

	done := runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-2",
			SourceText:  "second sentence",
			Text:        "第二句",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	pbx.assertNoMessage(t)

	done = runAsync(func() {
		api.processTranslationResult(ctx, conn, "conn-1", "user-1", model.TranslationResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			SourceText:  "first sentence",
			Text:        "第一句",
			IsFinal:     true,
			Engine:      "tmt",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	first := pbx.nextMessage(t)
	second := pbx.nextMessage(t)
	if first.UtteranceID != "utt-1" || first.Text != "第一句" || first.Sequence != 1 {
		t.Fatalf("unexpected first tts command: %#v", first)
	}
	if second.UtteranceID != "utt-2" || second.Text != "第二句" || second.Sequence != 2 {
		t.Fatalf("unexpected second tts command: %#v", second)
	}
}

func TestInterpreterSetDubbingDisabledStopsFutureAutoTTS(t *testing.T) {
	api, conn, client, pbx, llm, _ := newInterpreterTestHarness(t, "关闭后译文", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", true)
	conn.setDubbing("0")

	done := runAsync(func() {
		api.processASRResult(ctx, conn, "conn-1", "user-1", model.ASRResult{
			CallID:      "call-1",
			UtteranceID: "utt-1",
			Text:        "no dubbing",
			IsFinal:     true,
			Language:    "en",
		})
	})
	_ = readTestWSMessage(t, client)
	waitAsync(t, done)
	_ = llm.nextRequest(t)
	deepseek := readTestWSMessage(t, client)
	if deepseek.Type != "llm_tmt_final" || deepseek.Engine != "deepseek-flash" {
		t.Fatalf("unexpected deepseek message: %#v", deepseek)
	}
	pbx.assertNoMessage(t)
}

func TestInterpreterEndSessionMarksSQLiteEnded(t *testing.T) {
	api, _, _, _, _, store := newInterpreterTestHarness(t, "unused", nil)
	ctx := context.Background()
	api.createInterpreterSession(ctx, "conn-1", "call-1", "user-1", "tenant-a", "hybrid", false)

	active, err := store.Session(ctx, "call-1")
	if err != nil {
		t.Fatalf("load active session: %v", err)
	}
	if active.State != "active" {
		t.Fatalf("expected active session before close, got %#v", active)
	}

	api.endInterpreterSession(ctx, "conn-1")
	ended, err := store.Session(ctx, "call-1")
	if err != nil {
		t.Fatalf("load ended session: %v", err)
	}
	if ended.State != "ended" || ended.MediaState != "ended" || ended.EndedAt == "" {
		t.Fatalf("expected ended sqlite session, got %#v", ended)
	}
}

type interpreterTestLLM struct {
	result   string
	gate     chan struct{}
	requests chan llmpkg.Request
}

func (p *interpreterTestLLM) Name() string {
	return "deepseek-flash"
}

func (p *interpreterTestLLM) Translate(ctx context.Context, req llmpkg.Request) (llmpkg.Result, error) {
	p.requests <- req
	if p.gate != nil {
		select {
		case <-ctx.Done():
			return llmpkg.Result{}, ctx.Err()
		case <-p.gate:
		}
	}
	return llmpkg.Result{Text: p.result}, nil
}

func (p *interpreterTestLLM) nextRequest(t *testing.T) llmpkg.Request {
	t.Helper()
	select {
	case req := <-p.requests:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for llm request")
		return llmpkg.Request{}
	}
}

func (p *interpreterTestLLM) assertNoRequest(t *testing.T) {
	t.Helper()
	select {
	case req := <-p.requests:
		t.Fatalf("unexpected llm request: %#v", req)
	case <-time.After(50 * time.Millisecond):
	}
}

func (p *interpreterTestLLM) release() {
	if p.gate != nil {
		close(p.gate)
		p.gate = nil
	}
}

type recordingPBXControl struct {
	mu       sync.Mutex
	messages []pbxprotocol.Message
	ch       chan pbxprotocol.Message
}

func (p *recordingPBXControl) Send(ctx context.Context, message pbxprotocol.Message) error {
	p.mu.Lock()
	p.messages = append(p.messages, message)
	p.mu.Unlock()
	select {
	case p.ch <- message:
	default:
	}
	return ctx.Err()
}

func (p *recordingPBXControl) nextMessage(t *testing.T) pbxprotocol.Message {
	t.Helper()
	select {
	case message := <-p.ch:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pbx control message")
		return pbxprotocol.Message{}
	}
}

func (p *recordingPBXControl) assertNoMessage(t *testing.T) {
	t.Helper()
	select {
	case message := <-p.ch:
		t.Fatalf("unexpected pbx control message: %#v", message)
	case <-time.After(50 * time.Millisecond):
	}
}

func newInterpreterTestHarness(t *testing.T, llmResult string, llmGate chan struct{}) (*API, *wsConn, net.Conn, *recordingPBXControl, *interpreterTestLLM, *sessionstore.Store) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	storeDSN := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	store, err := sessionstore.Open(storeDSN)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if _, err := store.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("initialize sqlite store: %v", err)
	}

	llm := &interpreterTestLLM{result: llmResult, gate: llmGate, requests: make(chan llmpkg.Request, 8)}
	pbx := &recordingPBXControl{ch: make(chan pbxprotocol.Message, 8)}
	api := New(Dependencies{
		Gateway:      gateway.New(gateway.Options{}),
		PBXControl:   pbx,
		SessionStore: store,
		LLM:          llmpkg.NewClientWithProvider(llmpkg.Config{Provider: "deepseek"}, llm),
	})
	t.Cleanup(func() {
		api.mu.RLock()
		sessions := make([]*interpreterSession, 0, len(api.interpreters))
		for _, session := range api.interpreters {
			sessions = append(sessions, session)
		}
		api.mu.RUnlock()
		for _, session := range sessions {
			session.stopTTSCoordinator()
			session.waitStoreTasks()
		}
		_ = store.Close()
	})
	return api, &wsConn{netConn: server, reader: bufio.NewReader(server), writer: bufio.NewWriter(server), dubbing: true}, client, pbx, llm, store
}

func readTestWSMessage(t *testing.T, conn net.Conn) wsMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	opcode, payload, err := readWebSocketFrame(bufio.NewReader(conn), false)
	if err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("expected text frame, got opcode=%d", opcode)
	}
	var message wsMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode websocket message: %v", err)
	}
	return message
}

func assertNoWSMessage(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, _, err := readWebSocketFrame(bufio.NewReader(conn), false)
	if err == nil {
		t.Fatal("expected no websocket message")
	}
}

func assertStoreCount(t *testing.T, store *sessionstore.Store, model any, expected int64) {
	t.Helper()
	var count int64
	if err := store.DB().Model(model).Count(&count).Error; err != nil {
		t.Fatalf("count store rows: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d rows for %T, got %d", expected, model, count)
	}
}

func waitStoreCount(t *testing.T, store *sessionstore.Store, model any, expected int64) {
	t.Helper()
	waitUntil(t, func() bool {
		var count int64
		if err := store.DB().Model(model).Count(&count).Error; err != nil {
			return false
		}
		return count == expected
	})
}

func runAsync(fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	return done
}

func waitAsync(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async operation")
	}
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
