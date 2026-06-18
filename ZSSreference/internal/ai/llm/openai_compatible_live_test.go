//  大语言模型(LLM)翻译：OpenAI兼容接口→ DeepSeek Flash翻译→ 上下文+术语表纠错
package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleLiveQualityTranslation(t *testing.T) {
	if os.Getenv("SIMULSPEAK_LLM_LIVE_TEST") != "1" {
		t.Skip("set SIMULSPEAK_LLM_LIVE_TEST=1 to run live OpenAI-compatible LLM probe")
	}
	apiKey := liveEnv("SIMULSPEAK_LLM_API_KEY", "SIMULSPEAK_OPENAI_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY")
	endpoint := liveEnv("SIMULSPEAK_LLM_ENDPOINT", "SIMULSPEAK_OPENAI_ENDPOINT", "OPENAI_CHAT_COMPLETIONS_ENDPOINT", "DEEPSEEK_ENDPOINT")
	if endpoint == "" {
		endpoint = liveEnv("SIMULSPEAK_LLM_BASE_URL", "SIMULSPEAK_OPENAI_BASE_URL", "OPENAI_BASE_URL", "DEEPSEEK_BASE_URL")
	}
	model := liveEnv("SIMULSPEAK_LLM_MODEL", "SIMULSPEAK_OPENAI_MODEL", "OPENAI_MODEL", "DEEPSEEK_MODEL")
	if apiKey == "" {
		t.Fatal("missing LLM API key env")
	}
	if endpoint == "" {
		t.Fatal("missing LLM endpoint/base url env")
	}
	if model == "" {
		t.Fatal("missing LLM model env")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := NewClient(Config{
		Provider: "openai-compatible",
		APIKey:   apiKey,
		Endpoint: endpoint,
		Model:    model,
		Timeout:  18 * time.Second,
	})
	result, err := client.Translate(ctx, Request{
		CallID:          "live-llm-probe",
		UtteranceID:     "live-llm-probe-utt-1",
		Text:            "The call center agent should transfer the customer to the billing department.",
		Quality:         true,
		SourceContext:   []string{"The customer is asking about a charge on the bill."},
		Glossary:        map[string]string{"call center agent": "呼叫中心坐席", "billing department": "账单部门"},
		FastTranslation: "呼叫中心代理应该把客户转给计费部门。",
	})
	if err != nil {
		t.Fatalf("live openai-compatible translate failed: %v", err)
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatal("live openai-compatible returned empty translation")
	}
	t.Logf("live openai-compatible ok, target=%q terms=%d", result.Text, len(result.Terms))
}

func liveEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

