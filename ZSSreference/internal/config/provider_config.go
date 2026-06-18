//  AI Provider配置转换：从AppConfig提取各AI能力配置
package config

import (
	"strings"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

// ProviderConfigsFromAIConfig 将服务端 AI 配置转换为 WebSocket/媒体层使用的 provider 配置。
func ProviderConfigsFromAIConfig(ai AIConfig) map[model.CapabilityType]model.ProviderConfig {
	configs := map[model.CapabilityType]model.ProviderConfig{}
	if providerConfigEnabled(ai.ASR) {
		configs[model.CapabilityTypeASR] = providerConfigFromAIProvider(ai.ASR, model.CapabilityTypeASR)
	}
	if providerConfigEnabled(ai.TMT) {
		configs[model.CapabilityTypeTMT] = providerConfigFromAIProvider(ai.TMT, model.CapabilityTypeTMT)
	}
	if providerConfigEnabled(ai.TTS) {
		configs[model.CapabilityTypeTTS] = providerConfigFromAIProvider(ai.TTS, model.CapabilityTypeTTS)
	}
	if len(configs) == 0 {
		return nil
	}
	return configs
}

func providerConfigEnabled(provider AIProviderConfig) bool {
	name := strings.TrimSpace(provider.Provider)
	if name == "" {
		return false
	}
	if !strings.EqualFold(name, "mock") {
		return true
	}
	return provider.Endpoint != "" || provider.Model != "" || provider.APIKey != "" ||
		provider.AppID != "" || provider.SecretID != "" || provider.SecretKey != "" ||
		len(provider.Params) > 0
}

func providerConfigFromAIProvider(provider AIProviderConfig, capability model.CapabilityType) model.ProviderConfig {
	params := cloneStringMap(provider.Params)
	if params == nil {
		params = map[string]string{}
	}
	secrets := map[string]string{}
	if provider.AppID != "" {
		params["appId"] = provider.AppID
	}
	if provider.Model != "" {
		modelKey := "model"
		if capability == model.CapabilityTypeASR && params["engine_model_type"] == "" {
			modelKey = "engine_model_type"
		}
		params[modelKey] = provider.Model
	}
	if provider.APIKey != "" {
		secrets["apiKey"] = provider.APIKey
	}
	if provider.SecretID != "" {
		secrets["secretId"] = provider.SecretID
	}
	if provider.SecretKey != "" {
		secrets["secretKey"] = provider.SecretKey
	}
	return model.ProviderConfig{
		Provider: strings.TrimSpace(provider.Provider),
		Endpoint: strings.TrimSpace(provider.Endpoint),
		Params:   emptyStringMapToNil(params),
		Secrets:  emptyStringMapToNil(secrets),
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func emptyStringMapToNil(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return values
}

