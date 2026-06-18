//  语音合成(TTS)：腾讯云TTS→ WAV/PCM→ 重采样→ 中文配音
package tts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/tts"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test T T S_ Mock Provider_ Synthesize Chunks 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTTS_MockProvider_SynthesizeChunks(t *testing.T) {
	client := tts.NewClient(tts.Config{MaxSegmentRunes: 120})

	chunks, err := client.Synthesize(context.Background(), "hello world. second sentence.", tts.Options{CallID: "call-1", UtteranceID: "utt-1"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %#v", chunks)
	}
	if chunks[0].Sequence != 0 || chunks[1].Sequence != 1 || !chunks[1].IsLast {
		t.Fatalf("unexpected sequence: %#v", chunks)
	}
	if chunks[0].CallID != "call-1" || chunks[0].Format != "pcm" || chunks[0].SampleRate != 16000 {
		t.Fatalf("unexpected chunk metadata: %#v", chunks[0])
	}
}

// 作用: 验证 Test T T S_ First Chunk Latency Budget 场景的行为。
func TestTTS_FirstChunkLatencyBudget(t *testing.T) {
	client := tts.NewClient(tts.Config{FirstChunkDelay: 5 * time.Millisecond})

	if _, err := client.Synthesize(context.Background(), "hello", tts.Options{CallID: "call-1"}); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	latency := client.Metrics().FirstChunkLatency
	if latency <= 0 || latency > 50*time.Millisecond {
		t.Fatalf("first chunk latency outside budget: %s", latency)
	}
}

// 作用: 验证 Test T T S_ Cancel Releases Stream 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTTS_CancelReleasesStream(t *testing.T) {
	client := tts.NewClient(tts.Config{FirstChunkDelay: time.Millisecond, ChunkDelay: 25 * time.Millisecond, MaxSegmentRunes: 8})
	done := make(chan error, 1)

	// 作用: 并发执行 TTS 合成并把取消结果返回测试线程。
	go func() {
		_, err := client.Synthesize(context.Background(), "one two three four five six seven eight", tts.Options{CallID: "call-1"})
		done <- err
	}()
	waitForActive(t, client)
	client.Cancel("call-1")

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled, got %v", err)
	}
	if client.ActiveStreams() != 0 {
		t.Fatalf("expected stream released")
	}
	if client.Metrics().CancelCount != 1 {
		t.Fatalf("expected cancel metric, got %#v", client.Metrics())
	}
}

// 作用: 验证 Test T T S_ Provider Switch 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTTS_ProviderSwitch(t *testing.T) {
	config := tts.Config{Provider: "azure", DefaultVoice: "aria", DefaultFormat: "opus", DefaultRate: 48000}
	client := tts.NewClient(config)

	chunks, err := client.Synthesize(context.Background(), "hello", tts.Options{CallID: "call-1"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if client.Provider() != "azure" || chunks[0].Provider != "azure" {
		t.Fatalf("provider switch failed: client=%s chunk=%s", client.Provider(), chunks[0].Provider)
	}
	capability := tts.Capability("tts-1", config)
	if capability.Labels["provider"] != "azure" || capability.Labels["format"] != "opus" || capability.Models[0] != "aria" {
		t.Fatalf("unexpected capability: %#v", capability)
	}
}

// 作用: 验证 Test T T S_ Options Passthrough 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTTS_OptionsPassthrough(t *testing.T) {
	client := tts.NewClient(tts.Config{})
	options := tts.Options{
		CallID:      "call-1",
		UtteranceID: "utt-1",
		Voice:       "luna",
		Language:    "zh-CN",
		Speed:       1.25,
		Volume:      0.8,
		SampleRate:  48000,
		Format:      "opus",
	}

	chunks, err := client.Synthesize(context.Background(), "你好", options)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	got := chunks[0]
	if got.Voice != options.Voice || got.Language != options.Language || got.Speed != options.Speed || got.Volume != options.Volume {
		t.Fatalf("options not passed through: %#v", got)
	}
	if got.Format != "opus" || got.SampleRate != 48000 {
		t.Fatalf("audio options not passed through: %#v", got.TTSChunk)
	}
}

// 作用: 验证 Test T T S_ Text Segmentation 场景的行为。
func TestTTS_TextSegmentation(t *testing.T) {
	segments := tts.SegmentText("hello world. how are you? fine", 20)

	if len(segments) != 3 {
		t.Fatalf("unexpected segments: %#v", segments)
	}
	if segments[0] != "hello world." || segments[1] != "how are you?" || segments[2] != "fine" {
		t.Fatalf("unexpected segments: %#v", segments)
	}
}

// 作用: 验证 Test T T S_ Custom Provider Injection 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTTS_CustomProviderInjection(t *testing.T) {
	provider := &customTTSProvider{}
	client := tts.NewClientWithProvider(tts.Config{Provider: "third-party"}, provider)

	chunks, err := client.Synthesize(context.Background(), "hello", tts.Options{CallID: "call-1", Voice: "v1", Format: "opus", SampleRate: 48000})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if !provider.called || client.Provider() != "custom-tts" {
		t.Fatalf("expected custom provider to be used")
	}
	if len(chunks) != 1 || chunks[0].Provider != "custom-tts" || chunks[0].Voice != "v1" || chunks[0].Format != "opus" {
		t.Fatalf("unexpected custom provider chunks: %#v", chunks)
	}
}

// 作用: 处理 wait For Active 的核心流程。
// 逻辑: 先校验输入或上下文，再按分支和循环执行核心处理，最后返回结果或更新状态。
func waitForActive(t *testing.T, client *tts.Client) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if client.ActiveStreams() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for active stream")
}

type customTTSProvider struct {
	called bool
}

// 作用: 处理 Name 的核心流程。
func (p *customTTSProvider) Name() string {
	return "custom-tts"
}

// 作用: 处理 Synthesize 的核心流程。
func (p *customTTSProvider) Synthesize(_ context.Context, req tts.Request) ([]tts.Chunk, error) {
	p.called = true
	return []tts.Chunk{{
		TTSChunk: model.TTSChunk{
			CallID:     req.Options.CallID,
			Audio:      []byte("audio"),
			Format:     req.Options.Format,
			SampleRate: req.Options.SampleRate,
			IsLast:     true,
		},
		Text:     req.Text,
		Provider: p.Name(),
		Voice:    req.Options.Voice,
		Language: req.Options.Language,
	}}, nil
}

