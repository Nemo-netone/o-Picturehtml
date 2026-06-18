//  腾讯机器翻译(TMT)快翻：ASR final后快速产出中文草稿
package tmt

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTencentTMTLiveTextTranslate(t *testing.T) {
	if os.Getenv("SIMULSPEAK_TMT_LIVE_TEST") != "1" {
		t.Skip("set SIMULSPEAK_TMT_LIVE_TEST=1 to run live Tencent TMT probe")
	}
	secretID := strings.TrimSpace(os.Getenv("SIMULSPEAK_TENCENT_TMT_SECRETID"))
	secretKey := strings.TrimSpace(os.Getenv("SIMULSPEAK_TENCENT_TMT_SECRETKEY"))
	if secretID == "" || secretKey == "" {
		t.Fatal("missing SIMULSPEAK_TENCENT_TMT_SECRETID or SIMULSPEAK_TENCENT_TMT_SECRETKEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := NewClient(Config{
		Provider: "tencent-tmt",
		Endpoint: strings.TrimSpace(os.Getenv("SIMULSPEAK_TENCENT_TMT_ENDPOINT")),
		Region:   strings.TrimSpace(os.Getenv("SIMULSPEAK_TENCENT_TMT_REGION")),
		Params: map[string]string{
			"projectId": strings.TrimSpace(os.Getenv("SIMULSPEAK_TENCENT_TMT_PROJECT_ID")),
		},
		Secrets: map[string]string{
			"secretId":  secretID,
			"secretKey": secretKey,
		},
		Timeout: 10 * time.Second,
	})

	result, err := client.Translate(ctx, Request{
		CallID:      "live-tmt-probe",
		UtteranceID: "live-tmt-probe-utt-1",
		Text:        "hello world",
		SourceLang:  "en",
		TargetLang:  "zh",
		Quality:     true,
	})
	if err != nil {
		t.Fatalf("live tencent tmt translate failed: %v", err)
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatal("live tencent tmt returned empty TargetText")
	}
	t.Logf("live tencent tmt ok, target=%q", result.Text)
}

