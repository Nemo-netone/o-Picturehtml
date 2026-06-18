//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client_test

import (
	"testing"

	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
)

// TestTencentASRProviderConfig_OpusDropsInputSampleRate 验证 Opus WebRTC 场景不会发送 PCM 专用采样率。
func TestTencentASRProviderConfig_OpusDropsInputSampleRate(t *testing.T) {
	config := sdk.TencentASRProviderConfig(sdk.TencentASRConfig{
		AppID:           "1250000000",
		SecretID:        "secret-id",
		SecretKey:       "secret-key",
		EngineModelType: "16k_en",
		VoiceFormat:     "opus",
		InputSampleRate: 16000,
	})

	if _, ok := config.Params["input_sample_rate"]; ok {
		t.Fatalf("opus config should not include input_sample_rate: %#v", config.Params)
	}
}

// TestTencentASRProviderConfig_PCMKeepsOnly8000InputSampleRate 验证腾讯 PCM 只保留 8000 输入采样率。
func TestTencentASRProviderConfig_PCMKeepsOnly8000InputSampleRate(t *testing.T) {
	config := sdk.TencentASRProviderConfig(sdk.TencentASRConfig{
		VoiceFormat:     "pcm",
		InputSampleRate: 8000,
	})
	if config.Params["input_sample_rate"] != "8000" {
		t.Fatalf("expected 8000 sample rate, got %#v", config.Params)
	}

	config = sdk.TencentASRProviderConfig(sdk.TencentASRConfig{
		VoiceFormat:     "pcm",
		InputSampleRate: 16000,
	})
	if _, ok := config.Params["input_sample_rate"]; ok {
		t.Fatalf("16000 sample rate should be dropped: %#v", config.Params)
	}
}

