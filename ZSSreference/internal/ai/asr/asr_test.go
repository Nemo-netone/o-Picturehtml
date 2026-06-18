//  语音识别(ASR)抽象层：Provider接口→ Stream流式接口→ 注册机制→ 腾讯云实时ASR实现
package asr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/asr"
)

// 作用: 验证 Test A S R_ Mock Provider_ Partial Final 场景的行为。
func TestASR_MockProvider_PartialFinal(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "ok"})
	results, err := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-1", Payload: []byte("hello")}})
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if len(results) != 2 || results[0].IsFinal || !results[1].IsFinal {
		t.Fatalf("expected partial and final, got %#v", results)
	}
}

// 作用: 验证 Test A S R_ Auth Error 场景的行为。
func TestASR_AuthError(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "bad"})
	if _, err := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-1"}}); !errors.Is(err, asr.ErrAuth) {
		t.Fatalf("expected auth error, got %v", err)
	}
}

// 作用: 验证 Test A S R_ Retry Exhausted 场景的行为。
func TestASR_RetryExhausted(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "ok", FailuresBeforeSuccess: 3, MaxRetries: 2})
	if _, err := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-1"}}); !errors.Is(err, asr.ErrRetryExhausted) {
		t.Fatalf("expected retry exhausted, got %v", err)
	}
}

// 作用: 验证 Test A S R_ Timeout 场景的行为。
func TestASR_Timeout(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "ok", Delay: 20 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := client.StreamingRecognize(ctx, []asr.Frame{{CallID: "call-1"}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

// 作用: 验证 Test A S R_ Provider Switch 场景的行为。
func TestASR_ProviderSwitch(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock-alt", APIKey: "ok"})
	if client.Provider() != "mock-alt" {
		t.Fatalf("expected mock-alt provider")
	}
}

// 作用: 验证 Test A S R_ Low Confidence Filter 场景的行为。
func TestASR_LowConfidenceFilter(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "ok", MinConfidence: 0.9, MockConfidence: 0.5})
	results, err := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-1"}})
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected low confidence filtered, got %#v", results)
	}
}

// 作用: 验证 Test A S R_ Multi Call Isolation 场景的行为。
func TestASR_MultiCallIsolation(t *testing.T) {
	client := asr.NewClient(asr.Config{Provider: "mock", APIKey: "ok"})
	a, _ := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-a"}})
	b, _ := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-b"}})
	if a[1].CallID == b[1].CallID {
		t.Fatalf("expected separate call ids")
	}
}

// 作用: 验证 Test A S R_ Custom Provider Injection 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestASR_CustomProviderInjection(t *testing.T) {
	provider := &customASRProvider{}
	client := asr.NewClientWithProvider(asr.Config{Provider: "third-party"}, provider)

	results, err := client.StreamingRecognize(context.Background(), []asr.Frame{{CallID: "call-1"}})
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if !provider.called || client.Provider() != "custom-asr" {
		t.Fatalf("expected custom provider to be used")
	}
	if len(results) != 1 || results[0].Text != "third party transcript" {
		t.Fatalf("unexpected custom provider results: %#v", results)
	}
}

type customASRProvider struct {
	called bool
}

// 作用: 处理 Name 的核心流程。
func (p *customASRProvider) Name() string {
	return "custom-asr"
}

// 作用: 处理 Recognize 的核心流程。
func (p *customASRProvider) Recognize(_ context.Context, req asr.Request) ([]asr.Result, error) {
	p.called = true
	return []asr.Result{{CallID: req.Frames[0].CallID, Text: "third party transcript", IsFinal: true, Confidence: 0.99}}, nil
}

