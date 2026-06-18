//  腾讯机器翻译(TMT)快翻：ASR final后快速产出中文草稿
package tmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTencentTMTProviderTranslateSignsAndParsesResponse(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-TC-Action") != "TextTranslate" {
			t.Fatalf("unexpected action header: %s", r.Header.Get("X-TC-Action"))
		}
		if r.Header.Get("X-TC-Version") != "2018-03-21" {
			t.Fatalf("unexpected version header: %s", r.Header.Get("X-TC-Version"))
		}
		if r.Header.Get("X-TC-Region") != "ap-guangzhou" {
			t.Fatalf("unexpected region header: %s", r.Header.Get("X-TC-Region"))
		}
		if r.Header.Get("X-TC-Timestamp") != "1700000000" {
			t.Fatalf("unexpected timestamp header: %s", r.Header.Get("X-TC-Timestamp"))
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		expectedAuth := tencentTMTAuthorization(tencentTMTSignInput{
			SecretID:  "secret-id",
			SecretKey: "secret-key",
			Service:   "tmt",
			Host:      r.Host,
			Action:    "TextTranslate",
			Payload:   raw,
			Timestamp: now.Unix(),
		})
		if r.Header.Get("Authorization") != expectedAuth {
			t.Fatalf("authorization header mismatch:\n got: %s\nwant: %s", r.Header.Get("Authorization"), expectedAuth)
		}

		var body tencentTMTRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.SourceText != "hello world" || body.Source != "en" || body.Target != "zh" || body.ProjectID != 42 {
			t.Fatalf("unexpected request body: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"TargetText":"你好世界","Source":"en","Target":"zh","RequestId":"req-1"}}`))
	}))
	defer server.Close()

	provider := NewTencentTMTProvider(Config{
		Provider: "tencent-tmt",
		Endpoint: server.URL,
		Region:   "ap-guangzhou",
		Params:   map[string]string{"projectId": "42"},
		Secrets:  map[string]string{"secretId": "secret-id", "secretKey": "secret-key"},
		Timeout:  time.Second,
	})
	provider.client = server.Client()
	provider.now = func() time.Time { return now }

	result, err := provider.Translate(context.Background(), Request{
		CallID:      "call-1",
		UtteranceID: "utt-1",
		Text:        "hello world",
		SourceLang:  "en",
		TargetLang:  "zh",
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if result.Text != "你好世界" {
		t.Fatalf("translation = %q, want 你好世界", result.Text)
	}
}

func TestTencentTMTProviderReportsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"AuthFailure.SecretIdNotFound","Message":"missing secret id"},"RequestId":"req-error"}}`))
	}))
	defer server.Close()

	provider := NewTencentTMTProvider(Config{
		Provider: "tencent-tmt",
		Endpoint: server.URL,
		Secrets:  map[string]string{"secretId": "secret-id", "secretKey": "secret-key"},
	})
	provider.client = server.Client()
	provider.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }

	_, err := provider.Translate(context.Background(), Request{Text: "hello"})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !strings.Contains(err.Error(), "AuthFailure.SecretIdNotFound") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTencentTMTProviderKeepsTencentDefaultEndpoint(t *testing.T) {
	config := normalizeConfig(Config{Provider: "tencent-tmt"})
	if config.Endpoint != "" {
		t.Fatalf("tencent tmt should not inherit deepseek endpoint: %s", config.Endpoint)
	}
	built := buildTencentTMTConfig(config)
	if built.Endpoint != tencentTMTDefaultEndpoint {
		t.Fatalf("default endpoint = %s, want %s", built.Endpoint, tencentTMTDefaultEndpoint)
	}
	if built.Region != tencentTMTDefaultRegion {
		t.Fatalf("default region = %s, want %s", built.Region, tencentTMTDefaultRegion)
	}
}

