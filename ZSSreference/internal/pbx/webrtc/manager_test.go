// WebRTC管理器单元测试
package webrtc

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/asr"
	"github.com/SATA260/SimulSpeak1/internal/ai/tmt"
	"github.com/SATA260/SimulSpeak1/internal/ai/vad"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
	pionwebrtc "github.com/pion/webrtc/v4"
	"layeh.com/gopus"
)

// TestManager_AcceptOfferCreatesAnswer 验证后端能为音频 offer 创建 answer。
func TestManager_AcceptOfferCreatesAnswer(t *testing.T) {
	manager := NewManager()
	answer, err := manager.AcceptOffer(context.Background(), OfferRequest{
		ConnectionID: "conn-1",
		CallID:       "call-1",
		UserID:       "user-1",
		SDP:          newAudioOffer(t),
	})
	if err != nil {
		t.Fatalf("accept offer: %v", err)
	}
	t.Cleanup(func() {
		manager.CloseConnection("conn-1")
	})
	if !strings.Contains(answer, "m=audio") {
		t.Fatalf("expected audio answer, got %s", answer)
	}
}

func TestManager_ActiveConnections(t *testing.T) {
	manager := NewManager()
	if manager.ActiveConnections() != 0 {
		t.Fatalf("expected no active connections")
	}
	if _, err := manager.AcceptOffer(context.Background(), OfferRequest{
		ConnectionID: "conn-active",
		CallID:       "call-active",
		UserID:       "user-active",
		SDP:          newAudioOffer(t),
	}); err != nil {
		t.Fatalf("accept offer: %v", err)
	}
	if manager.ActiveConnections() != 1 {
		t.Fatalf("expected one active connection, got %d", manager.ActiveConnections())
	}
	manager.CloseConnection("conn-active")
	if manager.ActiveConnections() != 0 {
		t.Fatalf("expected active connections to return to zero, got %d", manager.ActiveConnections())
	}
}

// TestParseICECandidate 验证远端 ICE candidate 支持 JSON 和纯 candidate 字符串。
func TestParseICECandidate(t *testing.T) {
	jsonCandidate, err := parseICECandidate(`{"candidate":"candidate:1 1 udp 1 127.0.0.1 123 typ host","sdpMid":"0","sdpMLineIndex":0}`)
	if err != nil {
		t.Fatalf("parse json candidate: %v", err)
	}
	if jsonCandidate.Candidate == "" || jsonCandidate.SDPMid == nil || *jsonCandidate.SDPMid != "0" {
		t.Fatalf("unexpected json candidate: %#v", jsonCandidate)
	}

	rawCandidate, err := parseICECandidate("candidate:2 1 udp 1 127.0.0.1 124 typ host")
	if err != nil {
		t.Fatalf("parse raw candidate: %v", err)
	}
	if rawCandidate.Candidate == "" {
		t.Fatalf("unexpected raw candidate: %#v", rawCandidate)
	}
}

// TestSummarizeAudioSDP 验证音频 SDP 摘要能提取 m-line、方向和 codec，用于排查媒体协商问题。
func TestSummarizeAudioSDP(t *testing.T) {
	summary := summarizeAudioSDP(newAudioOffer(t))
	if summary.MediaCount != 1 {
		t.Fatalf("expected one audio media, got %#v", summary)
	}
	if len(summary.Directions) != 1 || summary.Directions[0] != "sendonly" {
		t.Fatalf("expected sendonly audio direction, got %#v", summary)
	}
	if len(summary.Codecs) == 0 {
		t.Fatalf("expected audio codecs, got %#v", summary)
	}
}

// TestSummarizeICECandidate 验证 ICE candidate 摘要能提取地址、端口、协议和类型。
func TestSummarizeICECandidate(t *testing.T) {
	summary := summarizeICECandidate("candidate:1 1 udp 2130706431 192.168.1.10 20001 typ host")
	if summary.Address != "192.168.1.10" || summary.Port != "20001" || summary.Protocol != "udp" || summary.Type != "host" {
		t.Fatalf("unexpected ice summary: %#v", summary)
	}
}

// TestSessionCandidateSnapshot 验证会话会缓存本地和远端 ICE candidate，并通过副本暴露给失败诊断日志。
func TestSessionCandidateSnapshot(t *testing.T) {
	session := &Session{}
	session.recordLocalICECandidate("candidate:1 1 udp 2130706431 192.168.1.10 20001 typ host")
	session.recordRemoteICECandidate("candidate:2 1 udp 2130706431 192.168.1.20 53367 typ srflx")

	snapshot := session.candidateSnapshot()
	if len(snapshot.Local) != 1 || snapshot.Local[0].Address != "192.168.1.10" {
		t.Fatalf("unexpected local candidates: %#v", snapshot.Local)
	}
	if len(snapshot.Remote) != 1 || snapshot.Remote[0].Address != "192.168.1.20" {
		t.Fatalf("unexpected remote candidates: %#v", snapshot.Remote)
	}

	snapshot.Local[0].Address = "mutated"
	fresh := session.candidateSnapshot()
	if fresh.Local[0].Address != "192.168.1.10" {
		t.Fatalf("candidate snapshot should be copied, got %#v", fresh.Local)
	}
}

// TestSession_ASRStreamWritesFrames 验证 WebRTC 会话会把音频 payload 连续写入同一个 ASR 实时流。
func TestSession_ASRStreamWritesFrames(t *testing.T) {
	provider := &testWebRTCASRProvider{}
	asr.RegisterProvider("webrtc-test-asr", func(config asr.Config) asr.Provider {
		return provider
	})
	results := make(chan model.ASRResult, 2)
	session := &Session{
		connectionID: "conn-1",
		callID:       "call-1",
		userID:       "user-1",
		asrClient: newASRClientFromProviderConfig(map[model.CapabilityType]model.ProviderConfig{
			model.CapabilityTypeASR: {Provider: "webrtc-test-asr"},
		}),
		onASRResult: func(result model.ASRResult) {
			results <- result
		},
	}

	session.queueASRFrame(context.Background(), bytes.Repeat([]byte("a"), 640), "audio/opus")
	session.queueASRFrame(context.Background(), bytes.Repeat([]byte("b"), 640), "audio/opus")

	select {
	case result := <-results:
		if result.Text != "识别成功" || !result.IsFinal || result.CallID != "call-1" {
			t.Fatalf("unexpected asr result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asr result")
	}
	session.closeASRStream(context.Background(), "test_done")
	if provider.openCount != 1 || provider.recognizeCount != 0 {
		t.Fatalf("expected one streaming open and no batch recognize: %#v", provider)
	}
	if provider.stream == nil || provider.stream.frameCount != 2 || provider.stream.audioBytes != 1280 || provider.stream.closed() != 1 {
		t.Fatalf("unexpected stream input: provider=%#v stream=%#v", provider, provider.stream)
	}
}

func TestSession_RecordingDoesNotRequireASR(t *testing.T) {
	store := storage.NewMemoryStorage()
	service := recording.NewService(store)
	recorder, started, err := recording.StartPCM16WAVRecorder(context.Background(), service, recording.PCM16WAVConfig{
		TenantID:    "tenant-a",
		CallID:      "call-1",
		RecordingID: "rec-1",
		SampleRate:  16000,
	})
	if err != nil {
		t.Fatalf("start recorder: %v", err)
	}
	session := &Session{
		connectionID:        "conn-1",
		tenantID:            "tenant-a",
		callID:              "call-1",
		userID:              "user-1",
		recording:           recorder,
		recordingSampleRate: 16000,
	}

	session.queueASRFrame(context.Background(), pcm16Frame(1200, 320), "audio/pcm")
	session.closeRecording(context.Background(), "test_done")

	object, err := store.Get(context.Background(), started.ObjectKey)
	if err != nil {
		t.Fatalf("get recording: %v", err)
	}
	if len(object.Data) != 44+640 || string(object.Data[:4]) != "RIFF" {
		t.Fatalf("unexpected recording data: len=%d header=%q", len(object.Data), object.Data[:4])
	}
}

// TestSession_ASRVoiceGateDropsSilenceBeforeSpeech 验证本地 VAD 会在语音开始前丢弃静音，避免无效 ASR 流。
func TestSession_ASRVoiceGateDropsSilenceBeforeSpeech(t *testing.T) {
	provider := &testWebRTCASRProvider{}
	session := newStreamingASRTestSession(t, "webrtc-vad-drop-asr", provider)
	session.asrGate = &asrVoiceGate{enabled: true, threshold: 0.01, startFrames: 2, endSilenceFrames: 2, preRollFrames: 2}

	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	if provider.openCount != 0 {
		t.Fatalf("silence should not open asr stream: %#v", provider)
	}

	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	if provider.openCount != 1 {
		t.Fatalf("speech should open one asr stream: %#v", provider)
	}
	if provider.stream == nil || provider.stream.frameCount != 2 {
		t.Fatalf("expected pre-roll speech frames written, stream=%#v", provider.stream)
	}
}

// TestSession_ASRVoiceGateClosesAndReopensUtterances 验证连续静音会结束当前 ASR 流，后续语音可重新建流。
func TestSession_ASRVoiceGateClosesAndReopensUtterances(t *testing.T) {
	provider := &testWebRTCASRProvider{}
	session := newStreamingASRTestSession(t, "webrtc-vad-reopen-asr", provider)
	session.asrGate = &asrVoiceGate{enabled: true, threshold: 0.01, startFrames: 1, endSilenceFrames: 2, preRollFrames: 1}

	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	first := provider.stream
	if provider.openCount != 1 || first == nil {
		t.Fatalf("expected first asr stream, provider=%#v", provider)
	}
	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	waitUntil(t, func() bool { return first.closed() == 1 })
	if first.closed() != 1 {
		t.Fatalf("expected vad endpoint to close first stream, closeCount=%d", first.closed())
	}

	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	if provider.openCount != 2 || provider.stream == first {
		t.Fatalf("expected second utterance to reopen stream, provider=%#v", provider)
	}
}

// TestSession_ASRVoiceGateUsesInjectedVADDetector 验证注入的 VAD detector 会实际驱动 ASR 开流和切句。
func TestSession_ASRVoiceGateUsesInjectedVADDetector(t *testing.T) {
	provider := &testWebRTCASRProvider{}
	session := newStreamingASRTestSession(t, "webrtc-vad-detector-asr", provider)
	detector, err := vad.NewDetector(vad.Config{
		Provider:        vad.ProviderSimple,
		SpeechThreshold: 0.5,
		SpeechMinFrames: 2,
		SilenceFrames:   2,
		WindowFrames:    2,
		WindowSpeech:    2,
	})
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	t.Cleanup(func() {
		_ = detector.Close()
	})
	session.asrGate = &asrVoiceGate{
		enabled:           true,
		vadSessionID:      session.connectionID,
		detector:          detector,
		detectorThreshold: detector.SpeechThreshold(),
		threshold:         1,
		startFrames:       2,
		endSilenceFrames:  2,
		preRollFrames:     2,
	}

	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	if provider.openCount != 0 {
		t.Fatalf("first speech frame should wait for detector speech_start, provider=%#v", provider)
	}
	session.queueASRFrame(context.Background(), pcm16Frame(2400, 320), "audio/pcm")
	if provider.openCount != 1 {
		t.Fatalf("detector speech_start should open asr stream, provider=%#v", provider)
	}
	if provider.stream == nil || provider.stream.frameCount != 2 {
		t.Fatalf("expected detector gate to write pre-roll frames, stream=%#v", provider.stream)
	}

	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	session.queueASRFrame(context.Background(), pcm16Frame(0, 320), "audio/pcm")
	waitUntil(t, func() bool { return provider.stream.closed() == 1 })
}

// TestASRVoiceGateDetectorPendingKeepsSpeechStart 验证 Silero 未凑满 512 samples 时不会重置已检测到的语音起始计数。
func TestASRVoiceGateDetectorPendingKeepsSpeechStart(t *testing.T) {
	gate := &asrVoiceGate{enabled: true, startFrames: 2, endSilenceFrames: 2, preRollFrames: 4}

	first := gate.evaluateSpeech(pcm16Frame(2400, 320), true, 0.2, 0.8, 0.5, "detector")
	if first.SpeechStart {
		t.Fatal("first speech frame should wait for another scored speech window")
	}
	if gate.speechFrames != 1 {
		t.Fatalf("expected one speech frame, got %d", gate.speechFrames)
	}

	pending := gate.evaluateDetectorPending(pcm16Frame(2400, 320), 0.2, 0.8, 0.5)
	if len(pending.WriteFrames) != 0 || gate.speechFrames != 1 {
		t.Fatalf("pending detector frame should only update pre-roll, decision=%#v speechFrames=%d", pending, gate.speechFrames)
	}

	second := gate.evaluateSpeech(pcm16Frame(2400, 320), true, 0.2, 0.8, 0.5, "detector")
	if !second.SpeechStart {
		t.Fatalf("second scored speech window should open gate, decision=%#v", second)
	}
	if len(second.WriteFrames) != 3 {
		t.Fatalf("expected pre-roll to include pending detector frame, got %d", len(second.WriteFrames))
	}
}

// TestSession_ASRForwardFiltersPartialByDefault 验证默认只向业务 WebSocket 回调最终 ASR 结果。
func TestSession_ASRForwardFiltersPartialByDefault(t *testing.T) {
	results := make(chan model.ASRResult, 2)
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { results <- result },
		asrResultMu:       sync.Mutex{},
		asrForwardPartial: false,
	}

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "中间结果", IsFinal: false, Confidence: 0.95}, "utt-1")
	assertNoASRResult(t, results)

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "最终结果", IsFinal: true, Confidence: 0.95}, "utt-1")
	result := <-results
	if result.Text != "最终结果" || !result.IsFinal {
		t.Fatalf("unexpected final result: %#v", result)
	}
}

// TestSession_ASRForwardAllowsPartialWhenConfigured 验证显式开启时 partial 会被回调，但重复 partial 仍会去重。
func TestSession_ASRForwardAllowsPartialWhenConfigured(t *testing.T) {
	results := make(chan model.ASRResult, 2)
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		asrForwardPartial: true,
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { results <- result },
	}

	partial := asr.Result{CallID: "call-1", Text: "中间结果", IsFinal: false, Confidence: 0.95}
	session.forwardASRResult(context.Background(), partial, "utt-1")
	session.forwardASRResult(context.Background(), partial, "utt-1")
	result := <-results
	if result.Text != "中间结果" || result.IsFinal {
		t.Fatalf("unexpected partial result: %#v", result)
	}
	assertNoASRResult(t, results)
}

// TestSession_ASRForwardDeduplicatesFinal 验证 provider 重放同一 final 时不会重复回调业务 WebSocket。
func TestSession_ASRForwardDeduplicatesFinal(t *testing.T) {
	results := make(chan model.ASRResult, 2)
	session := &Session{
		connectionID: "conn-1",
		callID:       "call-1",
		userID:       "user-1",
		asrForwarded: map[string]struct{}{},
		onASRResult:  func(result model.ASRResult) { results <- result },
	}

	final := asr.Result{CallID: "call-1", Text: "最终结果", IsFinal: true, Confidence: 0.95}
	session.forwardASRResult(context.Background(), final, "utt-1")
	session.forwardASRResult(context.Background(), final, "utt-1")
	result := <-results
	if result.Text != "最终结果" || !result.IsFinal {
		t.Fatalf("unexpected final result: %#v", result)
	}
	assertNoASRResult(t, results)
}

// TestSession_ASRStreamFinalAdvancesUtteranceID 验证同一个腾讯长连接 ASR stream 内，多句 final 不会复用同一个 utteranceId。
func TestSession_ASRStreamFinalAdvancesUtteranceID(t *testing.T) {
	results := make(chan model.ASRResult, 4)
	stream := &testWebRTCASRStream{
		callID:  "call-1",
		results: make(chan asr.Result, 4),
		errors:  make(chan error, 1),
	}
	stream.results <- asr.Result{CallID: "call-1", Text: "first", IsFinal: false, Confidence: 0.9}
	stream.results <- asr.Result{CallID: "call-1", Text: "first final", IsFinal: true, Confidence: 0.95}
	stream.results <- asr.Result{CallID: "call-1", Text: "second", IsFinal: false, Confidence: 0.9}
	stream.results <- asr.Result{CallID: "call-1", Text: "second final", IsFinal: true, Confidence: 0.95}
	close(stream.results)
	close(stream.errors)
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		utteranceSeq:      1,
		asrForwardPartial: true,
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { results <- result },
	}

	session.consumeASRStream(context.Background(), stream, "call-1-utt-1")

	firstPartial := <-results
	firstFinal := <-results
	secondPartial := <-results
	secondFinal := <-results
	if firstPartial.UtteranceID != "call-1-utt-1" || firstFinal.UtteranceID != "call-1-utt-1" {
		t.Fatalf("first utterance should keep initial id, partial=%#v final=%#v", firstPartial, firstFinal)
	}
	if secondPartial.UtteranceID != "call-1-utt-2" || secondFinal.UtteranceID != "call-1-utt-2" {
		t.Fatalf("second utterance should get a new id, partial=%#v final=%#v", secondPartial, secondFinal)
	}
}

// TestSession_ASRFinalTriggersTMTTranslation 验证 ASR final 转发后会触发 TMT 翻译回调。
func TestSession_ASRFinalTriggersTMTTranslation(t *testing.T) {
	asrResults := make(chan model.ASRResult, 1)
	translations := make(chan model.TranslationResult, 1)
	provider := &testWebRTCTMTProvider{requests: make(chan tmt.Request, 8)}
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { asrResults <- result },
		tmtClient:         tmt.NewClientWithProvider(tmt.Config{Provider: "webrtc-test-tmt", Timeout: time.Second}, provider),
		tmtForwarded:      map[string]struct{}{},
		onTranslation:     func(result model.TranslationResult) { translations <- result },
		asrForwardPartial: true,
	}

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "hello world", IsFinal: true, Confidence: 0.95}, "utt-1")

	asrResult := <-asrResults
	if asrResult.UtteranceID != "utt-1" || asrResult.Language != "en" {
		t.Fatalf("unexpected asr result: %#v", asrResult)
	}
	translation := waitTranslationResult(t, translations)
	if translation.UtteranceID != "utt-1" || translation.SourceText != "hello world" || translation.Text != "中文:hello world" {
		t.Fatalf("unexpected translation: %#v", translation)
	}
	if !translation.IsFinal || translation.Engine != "tmt" || translation.Revised {
		t.Fatalf("unexpected translation metadata: %#v", translation)
	}
	request := waitTranslationRequest(t, provider.requests)
	if request.SourceLang != "en" || request.TargetLang != "zh" || !request.Quality {
		t.Fatalf("unexpected tmt request: %#v", request)
	}
}

func TestSession_ASRAndTMTUseSessionLanguages(t *testing.T) {
	asrResults := make(chan model.ASRResult, 1)
	translations := make(chan model.TranslationResult, 1)
	provider := &testWebRTCTMTProvider{requests: make(chan tmt.Request, 8)}
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		sourceLanguage:    "zh",
		targetLanguage:    "en",
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { asrResults <- result },
		tmtClient:         tmt.NewClientWithProvider(tmt.Config{Provider: "webrtc-test-tmt", Timeout: time.Second}, provider),
		tmtForwarded:      map[string]struct{}{},
		onTranslation:     func(result model.TranslationResult) { translations <- result },
		asrForwardPartial: true,
	}

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "你好世界", IsFinal: true, Confidence: 0.95}, "utt-1")

	asrResult := <-asrResults
	if asrResult.Language != "zh" {
		t.Fatalf("unexpected asr language: %#v", asrResult)
	}
	translation := waitTranslationResult(t, translations)
	if translation.Language != "en" {
		t.Fatalf("unexpected translation language: %#v", translation)
	}
	request := waitTranslationRequest(t, provider.requests)
	if request.SourceLang != "zh" || request.TargetLang != "en" {
		t.Fatalf("unexpected tmt language direction: %#v", request)
	}
}

// TestSession_ASRPartialDoesNotTriggerTMTTranslation 验证 ASR partial 只转发英文字幕，不触发 TMT。
func TestSession_ASRPartialDoesNotTriggerTMTTranslation(t *testing.T) {
	asrResults := make(chan model.ASRResult, 1)
	translations := make(chan model.TranslationResult, 1)
	provider := &testWebRTCTMTProvider{requests: make(chan tmt.Request, 8)}
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		asrForwardPartial: true,
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { asrResults <- result },
		tmtClient:         tmt.NewClientWithProvider(tmt.Config{Provider: "webrtc-test-tmt", Timeout: time.Second}, provider),
		tmtForwarded:      map[string]struct{}{},
		onTranslation:     func(result model.TranslationResult) { translations <- result },
	}

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "hello one", IsFinal: false, Confidence: 0.8}, "utt-1")

	asrResult := <-asrResults
	if asrResult.UtteranceID != "utt-1" || asrResult.Text != "hello one" || asrResult.IsFinal {
		t.Fatalf("unexpected asr result: %#v", asrResult)
	}
	assertNoTranslationRequest(t, provider.requests)
	assertNoTranslationResult(t, translations)
}

// TestSession_TMTFailureDoesNotBlockASR 验证 TMT 调用失败时 ASR 结果仍会正常进入业务回调。
func TestSession_TMTFailureDoesNotBlockASR(t *testing.T) {
	asrResults := make(chan model.ASRResult, 1)
	translations := make(chan model.TranslationResult, 1)
	provider := &testWebRTCTMTProvider{err: tmt.ErrAuth, requests: make(chan tmt.Request, 8)}
	session := &Session{
		connectionID:      "conn-1",
		callID:            "call-1",
		userID:            "user-1",
		asrForwarded:      map[string]struct{}{},
		onASRResult:       func(result model.ASRResult) { asrResults <- result },
		tmtClient:         tmt.NewClientWithProvider(tmt.Config{Provider: "webrtc-test-tmt", Timeout: time.Second}, provider),
		tmtForwarded:      map[string]struct{}{},
		onTranslation:     func(result model.TranslationResult) { translations <- result },
		asrForwardPartial: true,
	}

	session.forwardASRResult(context.Background(), asr.Result{CallID: "call-1", Text: "hello world", IsFinal: true, Confidence: 0.95}, "utt-1")

	if result := <-asrResults; result.Text != "hello world" {
		t.Fatalf("unexpected asr result: %#v", result)
	}
	_ = waitTranslationRequest(t, provider.requests)
	assertNoTranslationResult(t, translations)
}

// TestShouldForwardASRPartial 验证 provider 参数可以显式开启 ASR partial 回传。
func TestShouldForwardASRPartial(t *testing.T) {
	configs := map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {Provider: "tencent-asr", Params: map[string]string{"forward_partial": "true"}},
	}
	if !shouldForwardASRPartial(configs) {
		t.Fatal("expected partial forwarding enabled")
	}
	configs[model.CapabilityTypeASR] = model.ProviderConfig{Provider: "tencent-asr"}
	if shouldForwardASRPartial(configs) {
		t.Fatal("expected partial forwarding disabled by default")
	}
}

// TestNormalizeTencentASRParamsForWebRTC 验证 WebRTC 上行音频转码后会固定按 PCM 调腾讯 ASR。
func TestNormalizeTencentASRParamsForWebRTC(t *testing.T) {
	params := map[string]string{
		"voice_format":      "opus",
		"input_sample_rate": "16000",
	}
	normalizeTencentASRParamsForWebRTCPcm(params)
	if params["voice_format"] != "pcm" {
		t.Fatalf("expected pcm voice format, got %#v", params)
	}
	if _, ok := params["input_sample_rate"]; ok {
		t.Fatalf("input_sample_rate should be removed for 16k pcm: %#v", params)
	}
}

// TestASRAudioProcessor_DecodesOpusToPCM16LE 验证 WebRTC Opus 包能被转成腾讯 ASR 可接收的 PCM16LE/16k。
func TestASRAudioProcessor_DecodesOpusToPCM16LE(t *testing.T) {
	encoder, err := gopus.NewEncoder(48000, 1, gopus.Voip)
	if err != nil {
		t.Fatalf("new opus encoder: %v", err)
	}
	input := make([]int16, 960)
	for index := range input {
		input[index] = int16(index % 300)
	}
	opusPacket, err := encoder.Encode(input, 960, 4000)
	if err != nil {
		t.Fatalf("encode opus: %v", err)
	}
	processor, err := newASRAudioProcessorFromProviderConfig(map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {Provider: "tencent-asr"},
	})
	if err != nil {
		t.Fatalf("new asr processor: %v", err)
	}

	pcm, err := processor.decodeOpusToPCM16LE(opusPacket)
	if err != nil {
		t.Fatalf("decode opus: %v", err)
	}
	if len(pcm) != 320*2 {
		t.Fatalf("expected 20ms 16k pcm bytes, got %d", len(pcm))
	}
}

// TestEncodePlaybackChunk_WAVPCM16ToPCMUFrames 验证 TTS wav/pcm 音频会被转成 WebRTC 可发送的 PCMU 帧。
func TestEncodePlaybackChunk_WAVPCM16ToPCMUFrames(t *testing.T) {
	samples := make([]int16, 320)
	for index := range samples {
		samples[index] = int16(index * 10)
	}
	frames, err := encodePlaybackChunk(PlaybackChunk{
		Payload: testWAVPCM16(t, 16000, samples),
		Format:  "wav",
	})
	if err != nil {
		t.Fatalf("encode wav: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected one 20ms pcmu frame, got %d", len(frames))
	}
	if len(frames[0].payload) != 160 || frames[0].duration != 20*time.Millisecond {
		t.Fatalf("unexpected pcmu frame: bytes=%d duration=%s", len(frames[0].payload), frames[0].duration)
	}
}

// TestEncodePlaybackChunk_RejectsMP3 验证不支持直接注入未解码 MP3，避免把不可播放数据写进 WebRTC。
func TestEncodePlaybackChunk_RejectsMP3(t *testing.T) {
	if _, err := encodePlaybackChunk(PlaybackChunk{Payload: []byte("mp3"), Format: "mp3", SampleRate: 16000}); err == nil {
		t.Fatal("expected mp3 inject error")
	}
}

func TestSession_OrderedTTSAudioWaitsForEarlierSequence(t *testing.T) {
	session := &Session{connectionID: "conn-1", callID: "call-1"}
	ctx := context.Background()

	second := []encodedAudioFrame{{payload: []byte("second"), duration: 20 * time.Millisecond}}
	if queued := session.enqueueOrderedTTSAudio(ctx, 2, second); !queued {
		t.Fatal("out-of-order tts should be queued")
	}
	if len(session.ttsQueue) != 0 {
		t.Fatalf("sequence 2 should wait for sequence 1, queue=%#v", session.ttsQueue)
	}

	first := []encodedAudioFrame{{payload: []byte("first"), duration: 20 * time.Millisecond}}
	if queued := session.enqueueOrderedTTSAudio(ctx, 1, first); !queued {
		t.Fatal("ordered tts should be queued before peer connection")
	}
	if len(session.ttsQueue) != 2 {
		t.Fatalf("expected both sequences released, got %d frames", len(session.ttsQueue))
	}
	if string(session.ttsQueue[0].payload) != "first" || string(session.ttsQueue[1].payload) != "second" {
		t.Fatalf("unexpected playback order: %#v", session.ttsQueue)
	}
}

func TestSession_OrderedTTSSkipReleasesLaterSequence(t *testing.T) {
	session := &Session{connectionID: "conn-1", callID: "call-1"}
	ctx := context.Background()

	second := []encodedAudioFrame{{payload: []byte("second"), duration: 20 * time.Millisecond}}
	_ = session.enqueueOrderedTTSAudio(ctx, 2, second)
	if len(session.ttsQueue) != 0 {
		t.Fatalf("sequence 2 should wait before skip, queue=%#v", session.ttsQueue)
	}

	session.skipOrderedTTSAudio(ctx, 1, "tts failed")
	if len(session.ttsQueue) != 1 || string(session.ttsQueue[0].payload) != "second" {
		t.Fatalf("skip sequence 1 should release sequence 2, queue=%#v", session.ttsQueue)
	}
}

// assertNoASRResult 确认短时间内没有 ASR 回调进入 channel。
func assertNoASRResult(t *testing.T, results <-chan model.ASRResult) {
	t.Helper()
	select {
	case result := <-results:
		t.Fatalf("unexpected asr result: %#v", result)
	case <-time.After(30 * time.Millisecond):
	}
}

func assertNoTranslationResult(t *testing.T, results <-chan model.TranslationResult) {
	t.Helper()
	select {
	case result := <-results:
		t.Fatalf("unexpected translation result: %#v", result)
	case <-time.After(30 * time.Millisecond):
	}
}

func assertNoTranslationRequest(t *testing.T, requests <-chan tmt.Request) {
	t.Helper()
	select {
	case request := <-requests:
		t.Fatalf("unexpected translation request: %#v", request)
	case <-time.After(30 * time.Millisecond):
	}
}

func waitTranslationResult(t *testing.T, results <-chan model.TranslationResult) model.TranslationResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for translation result")
		return model.TranslationResult{}
	}
}

func waitTranslationRequest(t *testing.T, requests <-chan tmt.Request) tmt.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for translation request")
		return tmt.Request{}
	}
}

// waitUntil 等待异步测试条件成立。
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

// newStreamingASRTestSession 创建启用流式 ASR 的测试会话。
func newStreamingASRTestSession(t *testing.T, providerName string, provider *testWebRTCASRProvider) *Session {
	t.Helper()
	asr.RegisterProvider(providerName, func(config asr.Config) asr.Provider {
		return provider
	})
	return &Session{
		connectionID: "conn-1",
		callID:       "call-1",
		userID:       "user-1",
		asrClient: newASRClientFromProviderConfig(map[model.CapabilityType]model.ProviderConfig{
			model.CapabilityTypeASR: {Provider: providerName},
		}),
		asrForwarded: map[string]struct{}{},
	}
}

// pcm16Frame 生成指定采样值的 PCM16LE 测试帧。
func pcm16Frame(sample int16, samples int) []byte {
	out := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		binary.LittleEndian.PutUint16(out[index*2:], uint16(sample))
	}
	return out
}

type testWebRTCASRProvider struct {
	openCount      int
	recognizeCount int
	stream         *testWebRTCASRStream
	streams        []*testWebRTCASRStream
}

// Name 返回测试 ASR provider 名称。
func (p *testWebRTCASRProvider) Name() string {
	return "webrtc-test-asr"
}

// Recognize 记录输入音频，并返回一个确定性最终结果。
func (p *testWebRTCASRProvider) Recognize(_ context.Context, req asr.Request) ([]asr.Result, error) {
	p.recognizeCount++
	return []asr.Result{{CallID: req.Frames[0].CallID, Text: "识别成功", IsFinal: true, Confidence: 0.99}}, nil
}

// OpenStream 创建测试 ASR 实时流，并记录打开次数。
func (p *testWebRTCASRProvider) OpenStream(_ context.Context, req asr.StreamRequest) (asr.Stream, error) {
	p.openCount++
	p.stream = &testWebRTCASRStream{
		callID:  req.CallID,
		results: make(chan asr.Result, 4),
		errors:  make(chan error, 1),
	}
	p.streams = append(p.streams, p.stream)
	return p.stream, nil
}

type testWebRTCASRStream struct {
	callID     string
	frameCount int
	audioBytes int
	closeCount atomic.Int32
	results    chan asr.Result
	errors     chan error
	once       sync.Once
}

type testWebRTCTMTProvider struct {
	err      error
	requests chan tmt.Request
}

func (p *testWebRTCTMTProvider) Name() string {
	return "webrtc-test-tmt"
}

func (p *testWebRTCTMTProvider) Translate(_ context.Context, req tmt.Request) (tmt.Result, error) {
	if p.requests == nil {
		p.requests = make(chan tmt.Request, 8)
	}
	p.requests <- req
	if p.err != nil {
		return tmt.Result{}, p.err
	}
	return tmt.Result{Text: "中文:" + req.Text}, nil
}

// Write 记录测试音频帧，并在首帧后发出一个确定性 ASR 结果。
func (s *testWebRTCASRStream) Write(_ context.Context, frame asr.Frame) error {
	s.frameCount++
	s.audioBytes += len(frame.Payload)
	if s.frameCount == 1 {
		s.results <- asr.Result{CallID: s.callID, Text: "识别成功", IsFinal: true, Confidence: 0.99}
	}
	return nil
}

// Close 关闭测试 ASR 实时流。
func (s *testWebRTCASRStream) Close(_ context.Context) error {
	s.closeCount.Add(1)
	s.once.Do(func() {
		close(s.results)
		close(s.errors)
	})
	return nil
}

func (s *testWebRTCASRStream) closed() int {
	return int(s.closeCount.Load())
}

// Results 返回测试 ASR 结果 channel。
func (s *testWebRTCASRStream) Results() <-chan asr.Result {
	return s.results
}

// Errors 返回测试 ASR 错误 channel。
func (s *testWebRTCASRStream) Errors() <-chan error {
	return s.errors
}

// newAudioOffer 创建测试用音频 offer。
func newAudioOffer(t *testing.T) string {
	t.Helper()
	peer, err := pionwebrtc.NewPeerConnection(pionwebrtc.Configuration{})
	if err != nil {
		t.Fatalf("new peer: %v", err)
	}
	t.Cleanup(func() {
		_ = peer.Close()
	})
	if _, err := peer.AddTransceiverFromKind(pionwebrtc.RTPCodecTypeAudio, pionwebrtc.RTPTransceiverInit{Direction: pionwebrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatalf("add transceiver: %v", err)
	}
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local description: %v", err)
	}
	return offer.SDP
}

// testWAVPCM16 构造测试用单声道 RIFF/WAVE PCM16 数据。
func testWAVPCM16(t *testing.T, sampleRate int, samples []int16) []byte {
	t.Helper()
	var data bytes.Buffer
	for _, sample := range samples {
		if err := binary.Write(&data, binary.LittleEndian, sample); err != nil {
			t.Fatalf("write sample: %v", err)
		}
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+data.Len()))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Len()))
	out.Write(data.Bytes())
	return out.Bytes()
}
