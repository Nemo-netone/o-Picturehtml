//  AI Provider配置模型
package model

// CloneProviderConfigs 深拷贝 provider 配置，避免调用方修改原始配置。
func CloneProviderConfigs(configs map[CapabilityType]ProviderConfig) map[CapabilityType]ProviderConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make(map[CapabilityType]ProviderConfig, len(configs))
	for typ, config := range configs {
		out[typ] = CloneProviderConfig(config)
	}
	return out
}

// CloneProviderConfig 深拷贝单个 provider 配置。
func CloneProviderConfig(config ProviderConfig) ProviderConfig {
	return ProviderConfig{
		Provider: config.Provider,
		Endpoint: config.Endpoint,
		Params:   cloneProviderStringMap(config.Params),
		Secrets:  cloneProviderStringMap(config.Secrets),
	}
}

// MergeProviderConfigs 将 defaults 与 overrides 合并，overrides 中的非空字段优先。
func MergeProviderConfigs(defaults, overrides map[CapabilityType]ProviderConfig) map[CapabilityType]ProviderConfig {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}
	out := CloneProviderConfigs(defaults)
	if out == nil {
		out = map[CapabilityType]ProviderConfig{}
	}
	for typ, override := range overrides {
		out[typ] = MergeProviderConfig(out[typ], override)
	}
	return out
}

// MergeProviderConfig 合并单个 provider 配置。若 override 显式切换 provider，则清空旧 provider 的 params/secrets。
func MergeProviderConfig(base, override ProviderConfig) ProviderConfig {
	if override.Provider != "" && base.Provider != "" && override.Provider != base.Provider {
		base = ProviderConfig{Provider: override.Provider}
	} else if override.Provider != "" {
		base.Provider = override.Provider
	}
	if override.Endpoint != "" {
		base.Endpoint = override.Endpoint
	}
	base.Params = mergeProviderStringMap(base.Params, override.Params)
	base.Secrets = mergeProviderStringMap(base.Secrets, override.Secrets)
	return base
}

func mergeProviderStringMap(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := cloneProviderStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func cloneProviderStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

