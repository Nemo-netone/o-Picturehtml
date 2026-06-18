//  语音合成(TTS)：腾讯云TTS→ WAV/PCM→ 重采样→ 中文配音
package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 作用: 验证腾讯 TTS provider 会发送 TC3 签名请求并解析 base64 音频。
// 逻辑: httptest.Server 检查请求头和 JSON body，返回模拟腾讯响应，断言 chunk 音频与元数据。
func TestTencentTTS_SynthesizeWithSignedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("X-TC-Action") != "TextToVoice" || r.Header.Get("X-TC-Version") != "2019-08-23" {
			t.Fatalf("missing tencent headers: %#v", r.Header)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=secret-id/") {
			t.Fatalf("missing authorization: %s", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["Text"] != "你好" || body["Codec"] != "wav" || int(body["SampleRate"].(float64)) != 16000 {
			t.Fatalf("unexpected body: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Audio":     base64.StdEncoding.EncodeToString([]byte("RIFF-audio")),
				"SessionId": body["SessionId"],
				"RequestId": "req-1",
			},
		})
	}))
	defer server.Close()

	config := Config{
		Provider:      "tencent-tts",
		Endpoint:      server.URL,
		DefaultVoice:  "101001",
		DefaultFormat: "wav",
		DefaultRate:   16000,
		Params: map[string]string{
			"codec":      "wav",
			"sampleRate": "16000",
			"region":     "ap-guangzhou",
			"language":   "zh-CN",
		},
		Secrets: map[string]string{
			"secretId":  "secret-id",
			"secretKey": "secret-key",
		},
	}
	provider := NewTencentProvider(config)
	provider.client = server.Client()
	provider.now = func() time.Time { return time.Unix(1700000000, 0) }
	client := NewClientWithProvider(config, provider)

	chunks, err := client.Synthesize(context.Background(), "你好", Options{CallID: "call-1", UtteranceID: "utt-1", Voice: "101001", Language: "zh-CN"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if len(chunks) != 1 || string(chunks[0].Audio) != "RIFF-audio" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
	if chunks[0].Provider != "tencent-tts" || chunks[0].Format != "wav" || chunks[0].SampleRate != 16000 {
		t.Fatalf("unexpected chunk metadata: %#v", chunks[0])
	}
}

// 作用: 验证腾讯 TTS API 错误会被转换成可观测错误。
func TestTencentTTS_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Error":     map[string]string{"Code": "AuthFailure", "Message": "bad secret"},
				"RequestId": "req-1",
			},
		})
	}))
	defer server.Close()
	config := Config{
		Provider:     "tencent-tts",
		Endpoint:     server.URL,
		DefaultVoice: "101001",
		Params:       map[string]string{"codec": "wav"},
		Secrets:      map[string]string{"secretId": "secret-id", "secretKey": "secret-key"},
	}
	provider := NewTencentProvider(config)
	provider.client = server.Client()
	client := NewClientWithProvider(config, provider)

	_, err := client.Synthesize(context.Background(), "你好", Options{CallID: "call-1", Voice: "101001"})
	if err == nil || !strings.Contains(err.Error(), "AuthFailure") {
		t.Fatalf("expected AuthFailure error, got %v", err)
	}
}

// 作用: 验证腾讯云 API v3 Authorization 在固定时间和 payload 下稳定生成。
func TestTencentCloudV3Authorization_Deterministic(t *testing.T) {
	auth := tencentCloudV3Authorization(tencentCloudV3Input{
		SecretID:  "secret-id",
		SecretKey: "secret-key",
		Service:   "tts",
		Host:      "tts.tencentcloudapi.com",
		Action:    "TextToVoice",
		Payload:   []byte(`{"Text":"你好"}`),
		Timestamp: 1700000000,
	})
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=secret-id/2023-11-14/tts/tc3_request") {
		t.Fatalf("unexpected credential scope: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host;x-tc-action") || !strings.Contains(auth, "Signature=") {
		t.Fatalf("unexpected authorization: %s", auth)
	}
}

