//  语音识别(ASR)抽象层：Provider接口→ Stream流式接口→ 注册机制→ 腾讯云实时ASR实现
package asr

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

// 作用: 验证腾讯 ASR 签名 URL 按官方要求包含 AppID、排序 query 和脱敏日志预览。
func TestTencentASR_BuildSignedURL(t *testing.T) {
	config := tencentASRConfig{
		AppID:           "1400000000",
		SecretID:        "secret-id",
		SecretKey:       "secret-key",
		Endpoint:        "wss://asr.cloud.tencent.com/asr/v2",
		EngineModelType: "16k_en",
		VoiceFormat:     "10",
		NeedVAD:         "1",
		Params: map[string]string{
			"filter_dirty": "0",
		},
	}
	now := time.Unix(1700000000, 0)

	rawURL, summary, err := buildTencentASRSignedURL(config, now, "voice-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if !strings.HasPrefix(rawURL, "wss://asr.cloud.tencent.com/asr/v2/1400000000?") {
		t.Fatalf("unexpected url prefix: %s", rawURL)
	}
	if !strings.Contains(rawURL, "engine_model_type=16k_en") || !strings.Contains(rawURL, "voice_format=10") {
		t.Fatalf("expected query params in url: %s", rawURL)
	}
	if strings.Contains(summary, "secret-key") || strings.Contains(summary, "secret-id") || !strings.Contains(summary, "signature=[REDACTED]") || !strings.Contains(summary, "secretid=[REDACTED]") {
		t.Fatalf("summary should be redacted: %s", summary)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse signed url: %v", err)
	}
	if parsed.Query().Get("signature") == "" {
		t.Fatalf("expected signature query: %s", rawURL)
	}
}

// TestTencentASR_OpusDropsInputSampleRate 验证 Opus 格式不会透传 PCM 专用 input_sample_rate。
func TestTencentASR_OpusDropsInputSampleRate(t *testing.T) {
	config := buildTencentASRConfig(Config{
		Provider: "tencent-asr",
		Params: map[string]string{
			"appId":             "1400000000",
			"engine_model_type": "16k_en",
			"voice_format":      "opus",
			"input_sample_rate": "16000",
		},
		Secrets: map[string]string{
			"secretId":  "secret-id",
			"secretKey": "secret-key",
		},
	})

	rawURL, _, err := buildTencentASRSignedURL(config, time.Unix(1700000000, 0), "voice-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if strings.Contains(rawURL, "input_sample_rate=") {
		t.Fatalf("opus url should not contain input_sample_rate: %s", rawURL)
	}
	if !strings.Contains(rawURL, "voice_format=10") {
		t.Fatalf("expected opus voice_format=10: %s", rawURL)
	}
}

// TestTencentASR_PCMKeepsOnly8000InputSampleRate 验证 PCM 只有 8000 采样率会透传给腾讯 ASR。
func TestTencentASR_PCMKeepsOnly8000InputSampleRate(t *testing.T) {
	config := buildTencentASRConfig(Config{
		Provider: "tencent-asr",
		Params: map[string]string{
			"appId":             "1400000000",
			"engine_model_type": "16k_en",
			"voice_format":      "pcm",
			"input_sample_rate": "8000",
		},
		Secrets: map[string]string{
			"secretId":  "secret-id",
			"secretKey": "secret-key",
		},
	})
	rawURL, _, err := buildTencentASRSignedURL(config, time.Unix(1700000000, 0), "voice-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if !strings.Contains(rawURL, "input_sample_rate=8000") {
		t.Fatalf("expected 8000 input_sample_rate: %s", rawURL)
	}

	config = buildTencentASRConfig(Config{
		Provider: "tencent-asr",
		Params: map[string]string{
			"appId":             "1400000000",
			"engine_model_type": "16k_en",
			"voice_format":      "pcm",
			"input_sample_rate": "16000",
		},
		Secrets: map[string]string{
			"secretId":  "secret-id",
			"secretKey": "secret-key",
		},
	})
	rawURL, _, err = buildTencentASRSignedURL(config, time.Unix(1700000000, 0), "voice-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if strings.Contains(rawURL, "input_sample_rate=") {
		t.Fatalf("16000 input_sample_rate should be dropped: %s", rawURL)
	}
}

// 作用: 验证腾讯 ASR provider 会发送音频帧、结束消息，并解析最终识别结果。
// 逻辑: 用 fake WebSocket 返回启动确认、partial、final 和 complete，断言输出文本与写入帧。
func TestTencentASR_RecognizeWithFakeWebSocket(t *testing.T) {
	fake := &fakeTencentASRConn{
		responses: []tencentASRResponse{
			{Code: 0, Message: "success", VoiceID: "voice-1"},
			{Code: 0, Result: tencentASRResultObject{SliceType: 1, VoiceTextStr: "你"}},
			{Code: 0, Result: tencentASRResultObject{SliceType: 2, VoiceTextStr: "你好"}},
			{Code: 0, Final: 1},
		},
	}
	provider := NewTencentProvider(Config{Provider: "tencent-asr"})
	provider.now = func() time.Time { return time.Unix(1700000000, 0) }
	provider.voice = func() string { return "voice-1" }
	provider.dial = func(_ context.Context, rawURL string) (tencentASRConn, error) {
		if !strings.Contains(rawURL, "signature=") {
			t.Fatalf("expected signed url, got %s", rawURL)
		}
		return fake, nil
	}

	client := NewClientWithProvider(Config{
		Provider: "tencent-asr",
		Params: map[string]string{
			"appId":             "1400000000",
			"engine_model_type": "16k_en",
			"voice_format":      "opus",
		},
		Secrets: map[string]string{
			"secretId":  "secret-id",
			"secretKey": "secret-key",
		},
	}, provider)
	results, err := client.StreamingRecognize(context.Background(), []Frame{
		{CallID: "call-1", Payload: []byte{0x01, 0x02}},
		{CallID: "call-1", Payload: []byte{0x03}},
	})
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if len(results) != 2 || results[1].Text != "你好" || !results[1].IsFinal {
		t.Fatalf("unexpected results: %#v", results)
	}
	if len(fake.binaryWrites) != 2 || string(fake.textWrites[0]) != `{"type":"end"}` {
		t.Fatalf("unexpected websocket writes: binary=%#v text=%q", fake.binaryWrites, fake.textWrites)
	}
	if string(fake.binaryWrites[0]) != "\x01\x02" || string(fake.binaryWrites[1]) != "\x03" {
		t.Fatalf("expected raw opus payload writes, got %#v", fake.binaryWrites)
	}
	if !fake.closed {
		t.Fatalf("expected websocket closed")
	}
}

// TestTencentASR_OpenStreamWithFakeWebSocket 验证腾讯 ASR 长连接实时流只建一次连接并连续写入多帧音频。
// 逻辑: fake WebSocket 先返回启动确认，再返回 partial/final/final；测试直接 OpenStream、Write 两帧、Close 一次。
func TestTencentASR_OpenStreamWithFakeWebSocket(t *testing.T) {
	fake := &fakeTencentASRConn{
		responses: []tencentASRResponse{
			{Code: 0, Message: "success", VoiceID: "voice-1"},
			{Code: 0, Result: tencentASRResultObject{SliceType: 1, VoiceTextStr: "测"}},
			{Code: 0, Result: tencentASRResultObject{SliceType: 2, VoiceTextStr: "测试"}},
			{Code: 0, Final: 1},
		},
	}
	var dialCount int
	provider := NewTencentProvider(Config{Provider: "tencent-asr"})
	provider.now = func() time.Time { return time.Unix(1700000000, 0) }
	provider.voice = func() string { return "voice-1" }
	provider.dial = func(_ context.Context, rawURL string) (tencentASRConn, error) {
		dialCount++
		if !strings.Contains(rawURL, "voice_format=1") {
			t.Fatalf("expected pcm voice format in url, got %s", rawURL)
		}
		return fake, nil
	}

	stream, err := provider.OpenStream(context.Background(), StreamRequest{
		CallID: "call-1",
		Config: Config{
			Provider: "tencent-asr",
			Params: map[string]string{
				"appId":             "1400000000",
				"engine_model_type": "16k_en",
				"voice_format":      "pcm",
			},
			Secrets: map[string]string{
				"secretId":  "secret-id",
				"secretKey": "secret-key",
			},
		},
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Write(context.Background(), Frame{CallID: "call-1", Payload: []byte{0x01, 0x02}}); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	if err := stream.Write(context.Background(), Frame{CallID: "call-1", Payload: []byte{0x03}}); err != nil {
		t.Fatalf("write second frame: %v", err)
	}
	if err := stream.Close(context.Background()); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	results, err := collectASRStreamOutputs(stream)
	if err != nil {
		t.Fatalf("collect stream outputs: %v", err)
	}
	if dialCount != 1 {
		t.Fatalf("expected one websocket dial, got %d", dialCount)
	}
	if len(fake.binaryWrites) != 2 || len(fake.textWrites) != 1 || string(fake.textWrites[0]) != `{"type":"end"}` {
		t.Fatalf("unexpected websocket writes: binary=%#v text=%q", fake.binaryWrites, fake.textWrites)
	}
	if len(results) != 2 || results[1].Text != "测试" || !results[1].IsFinal {
		t.Fatalf("unexpected stream results: %#v", results)
	}
}

type fakeTencentASRConn struct {
	responses    []tencentASRResponse
	readIndex    int
	binaryWrites [][]byte
	textWrites   [][]byte
	closed       bool
}

// ReadJSON 将预置腾讯 ASR 响应写入调用方传入的结构体。
func (c *fakeTencentASRConn) ReadJSON(v any) error {
	payload, _ := json.Marshal(c.responses[c.readIndex])
	c.readIndex++
	return json.Unmarshal(payload, v)
}

// WriteBinary 记录测试音频二进制帧。
func (c *fakeTencentASRConn) WriteBinary(payload []byte) error {
	c.binaryWrites = append(c.binaryWrites, append([]byte(nil), payload...))
	return nil
}

// WriteText 记录测试文本控制帧。
func (c *fakeTencentASRConn) WriteText(payload []byte) error {
	c.textWrites = append(c.textWrites, append([]byte(nil), payload...))
	return nil
}

// Close 标记 fake WebSocket 已关闭。
func (c *fakeTencentASRConn) Close() error {
	c.closed = true
	return nil
}

