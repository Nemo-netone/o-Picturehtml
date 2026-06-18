// 大语言模型(LLM)翻译：OpenAI兼容接口→ DeepSeek Flash翻译→ 上下文+术语表纠错
package llm

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

func TestOpenAICompatibleProviderQualityTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body deepseekRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Model != "test-model" || body.Stream {
			t.Fatalf("unexpected model/stream: %#v", body)
		}
		if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
			t.Fatalf("quality translation should request json_object: %#v", body.ResponseFormat)
		}
		joinedMessages := ""
		for _, message := range body.Messages {
			joinedMessages += message.Content + "\n"
		}
		for _, want := range []string{"Previous ASR final transcripts", "Previous sentence", "SimulSpeak => 同声说", "Current machine translation draft", "NEW English sentence"} {
			if !strings.Contains(joinedMessages, want) {
				t.Fatalf("messages missing %q: %s", want, joinedMessages)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"translation\":\"你好，世界\",\"terms\":{\"SimulSpeak\":\"同声说\"}}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(Config{
		Provider: "openai-compatible",
		APIKey:   "test-key",
		Endpoint: server.URL,
		Model:    "test-model",
		Timeout:  time.Second,
	})
	provider.http = server.Client()

	result, err := provider.Translate(context.Background(), Request{
		Text:            "hello world",
		Quality:         true,
		SourceContext:   []string{"Previous sentence"},
		Glossary:        map[string]string{"SimulSpeak": "同声说"},
		FastTranslation: "你好世界",
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if result.Text != "你好，世界" || result.Terms["SimulSpeak"] != "同声说" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestOpenAICompatibleProviderUsesRequestedLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body deepseekRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		joinedMessages := ""
		for _, message := range body.Messages {
			joinedMessages += message.Content + "\n"
		}
		for _, want := range []string{"Chinese talk", "into natural, fluent English", "NEW Chinese sentence"} {
			if !strings.Contains(joinedMessages, want) {
				t.Fatalf("messages missing %q: %s", want, joinedMessages)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"translation\":\"hello world\",\"terms\":{}}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(Config{
		Provider: "openai-compatible",
		APIKey:   "test-key",
		Endpoint: server.URL,
		Model:    "test-model",
		Timeout:  time.Second,
	})
	provider.http = server.Client()

	result, err := provider.Translate(context.Background(), Request{
		Text:       "你好世界",
		SourceLang: "zh",
		TargetLang: "en",
		Quality:    true,
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if result.Text != "hello world" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestOpenAICompatibleProviderExtractVocabulary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body deepseekRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
			t.Fatalf("vocabulary extraction should request json_object: %#v", body.ResponseFormat)
		}
		joinedMessages := ""
		for _, message := range body.Messages {
			joinedMessages += message.Content + "\n"
		}
		for _, want := range []string{"English vocabulary teacher", "Return at most 3 entries", "[utt-1] We use a transformer architecture."} {
			if !strings.Contains(joinedMessages, want) {
				t.Fatalf("messages missing %q: %s", want, joinedMessages)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"entries\":[{\"word\":\"architecture\",\"lemma\":\"architecture\",\"phonetic\":\"/x/\",\"partOfSpeech\":\"noun\",\"meaningZh\":\"架构\",\"exampleEn\":\"We use a transformer architecture.\",\"exampleZh\":\"我们使用 Transformer 架构。\",\"occurrences\":1,\"difficulty\":\"B2\",\"sourceUtteranceIds\":[\"utt-1\"]}]}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(Config{
		Provider: "openai-compatible",
		APIKey:   "test-key",
		Endpoint: server.URL,
		Model:    "test-model",
		Timeout:  time.Second,
	})
	provider.http = server.Client()

	result, err := provider.ExtractVocabulary(context.Background(), VocabularyRequest{
		TaskID:   "task-1",
		Texts:    []VocabularyText{{UtteranceID: "utt-1", Text: "We use a transformer architecture."}},
		MaxWords: 3,
	})
	if err != nil {
		t.Fatalf("extract vocabulary: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Word != "architecture" || result.Entries[0].SourceUtteranceIDs[0] != "utt-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.RawRequestJSON, "test-model") || !strings.Contains(result.RawResponseJSON, "architecture") {
		t.Fatalf("expected raw request/response, got %#v", result)
	}
}

func TestOpenAICompatibleProviderRequiresEndpointAndModel(t *testing.T) {
	provider := NewOpenAICompatibleProvider(Config{
		Provider: "openai-compatible",
		APIKey:   "test-key",
	})

	_, err := provider.Translate(context.Background(), Request{Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "endpoint is empty") {
		t.Fatalf("expected missing endpoint error, got %v", err)
	}

	provider = NewOpenAICompatibleProvider(Config{
		Provider: "openai-compatible",
		APIKey:   "test-key",
		Endpoint: "https://example.com/v1/chat/completions",
	})
	_, err = provider.Translate(context.Background(), Request{Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "model is empty") {
		t.Fatalf("expected missing model error, got %v", err)
	}
}

func TestNormalizeOpenAIChatEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com":                     "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/":                    "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1":                  "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1/chat/completions": "https://api.deepseek.com/v1/chat/completions",
	}
	for input, want := range cases {
		if got := normalizeOpenAIChatEndpoint(input); got != want {
			t.Fatalf("normalizeOpenAIChatEndpoint(%q) = %q, want %q", input, got, want)
		}
	}
}
