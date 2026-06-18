//  大语言模型(LLM)翻译：OpenAI兼容接口→ DeepSeek Flash翻译→ 上下文+术语表纠错
package llm

import (
	"context"
	"testing"
)

func TestMockTranslateFastAndQuality(t *testing.T) {
	client := NewClient(Config{Provider: "mock"})

	fast, err := client.Translate(context.Background(), Request{Text: "hello world", Quality: false})
	if err != nil {
		t.Fatalf("fast translate: %v", err)
	}
	if fast.Text != "[译]hello world" {
		t.Fatalf("unexpected fast text: %q", fast.Text)
	}

	quality, err := client.Translate(context.Background(), Request{Text: "hello world", Quality: true})
	if err != nil {
		t.Fatalf("quality translate: %v", err)
	}
	if quality.Text != "[重译]hello world" {
		t.Fatalf("unexpected quality text: %q", quality.Text)
	}
}

func TestTranslateAuthError(t *testing.T) {
	client := NewClient(Config{Provider: "mock", APIKey: "bad"})
	if _, err := client.Translate(context.Background(), Request{Text: "x"}); err != ErrAuth {
		t.Fatalf("expected ErrAuth, got %v", err)
	}
}

func TestParseQualityContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		terms   int
	}{
		{"plain json", `{"translation":"你好","terms":{"world":"世界"}}`, "你好", 1},
		{"fenced json", "```json\n{\"translation\":\"你好\",\"terms\":{}}\n```", "你好", 0},
		{"not json fallback", "你好世界", "你好世界", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseQualityContent(tc.content)
			if got.Text != tc.want {
				t.Fatalf("text = %q, want %q", got.Text, tc.want)
			}
			if len(got.Terms) != tc.terms {
				t.Fatalf("terms = %d, want %d", len(got.Terms), tc.terms)
			}
		})
	}
}

