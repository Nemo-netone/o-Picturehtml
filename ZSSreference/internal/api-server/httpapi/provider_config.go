//  AI Provider配置处理：配置转换与校验
package httpapi

import (
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/security"
)

// redactProviderConfig 脱敏 provider 参数和密钥，Params 中 key 名带 secret/key/token 的值也会被替换。
func redactProviderConfig(config *model.ProviderConfig) *model.ProviderConfig {
	if config == nil {
		return nil
	}
	return &model.ProviderConfig{
		Provider: config.Provider,
		Endpoint: config.Endpoint,
		Params:   security.RedactMap(config.Params),
		Secrets:  redactAll(config.Secrets),
	}
}

// redactProviderConfigMap 脱敏 WebSocket hello ack 中的 provider 配置映射。
func redactProviderConfigMap(configs map[model.CapabilityType]model.ProviderConfig) map[model.CapabilityType]model.ProviderConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make(map[model.CapabilityType]model.ProviderConfig, len(configs))
	for typ, config := range configs {
		redacted := redactProviderConfig(&config)
		if redacted != nil {
			out[typ] = *redacted
		}
	}
	return out
}

// redactAll 将 map 中所有值替换为 [REDACTED]，用于 Secrets 这类天然敏感字段。
func redactAll(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key := range values {
		out[key] = "[REDACTED]"
	}
	return out
}

