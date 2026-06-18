// Package client 是 SimulSpeak 的 PBX 节点客户端库（SDK）。
//
// 本文件实现 AI Provider 配置构建器：将腾讯云 ASR/TTS 的业务参数转换为 PBX 节点
// 所需的标准化 ProviderConfig，通过函数式选项注入到 Client 中，随 WebSocket client_hello 上报。
package client

import (
	"strconv"
	"strings"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 内置 provider 名称常量
const (
	// TencentASRProvider 腾讯云实时语音识别 provider 标识。
	TencentASRProvider = "tencent-asr"
	// TencentTTSProvider 腾讯云语音合成 provider 标识。
	TencentTTSProvider = "tencent-tts"
)

// TencentASRConfig 腾讯云实时语音识别(ASR)的完整业务参数配置。
type TencentASRConfig struct {
	AppID           string // 腾讯云 AppID
	SecretID        string // 腾讯云 SecretID
	SecretKey       string // 腾讯云 SecretKey
	Endpoint        string // ASR WebSocket 端点（默认 wss://asr.cloud.tencent.com/asr/v2）
	EngineModelType string // 引擎模型类型（如 16k_en）
	VoiceFormat     string // 音频格式（如 pcm）
	InputSampleRate int    // 输入采样率（仅 PCM 8k 上采样场景需要设置）
	NeedVAD         bool   // 是否启用服务端 VAD（通常关闭，由 PBX 本地 VAD 控制）
	FilterDirty     int    // 过滤脏词
	FilterModal     int    // 过滤语气词
	FilterPunc      int    // 过滤标点
	ConvertNumMode  int    // 数字转换模式
	WordInfo        int    // 词级别时间戳信息
}

// TencentTTSConfig 腾讯云语音合成(TTS)的完整业务参数配置。
type TencentTTSConfig struct {
	AppID      string  // 腾讯云 AppID
	SecretID   string  // 腾讯云 SecretID
	SecretKey  string  // 腾讯云 SecretKey
	Endpoint   string  // TTS API 端点（默认 https://tts.tencentcloudapi.com）
	VoiceType  string  // 音色类型
	Codec      string  // 音频编码格式
	SampleRate int     // 采样率
	Speed      float64 // 语速
	Volume     float64 // 音量
	Language   string  // 语言（如 zh-CN）
}

// WithProviderConfig 设置某类 AI 能力使用的第三方 provider 参数。
// WebSocket 连接 PBX 时会随 client_hello 上报这些配置，PBX 据此初始化对应的 AI 能力。
func WithProviderConfig(typ model.CapabilityType, config model.ProviderConfig) Option {
	return func(c *Client) {
		if typ == "" {
			return
		}
		c.providerConfigs[typ] = cloneProviderConfig(config)
	}
}

// WithTencentASR 便捷设置腾讯云实时语音识别 provider 参数。
func WithTencentASR(config TencentASRConfig) Option {
	return WithProviderConfig(model.CapabilityTypeASR, TencentASRProviderConfig(config))
}

// WithTencentTTS 便捷设置腾讯云语音合成 provider 参数。
func WithTencentTTS(config TencentTTSConfig) Option {
	return WithProviderConfig(model.CapabilityTypeTTS, TencentTTSProviderConfig(config))
}

// TencentASRProviderConfig 将腾讯云实时 ASR 业务参数转换为 PBX provider 标准化配置。
// 这是业务参数到 PBX 内部协议的适配层。
func TencentASRProviderConfig(config TencentASRConfig) model.ProviderConfig {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "wss://asr.cloud.tencent.com/asr/v2"
	}
	// 构建 ASR 参数映射
	params := map[string]string{}
	putString(params, "appId", config.AppID)
	putString(params, "engine_model_type", config.EngineModelType)
	putString(params, "voice_format", config.VoiceFormat)
	putTencentASRInputSampleRate(params, config.VoiceFormat, config.InputSampleRate)
	putBool(params, "needvad", config.NeedVAD)
	putInt(params, "filter_dirty", config.FilterDirty)
	putInt(params, "filter_modal", config.FilterModal)
	putInt(params, "filter_punc", config.FilterPunc)
	putInt(params, "convert_num_mode", config.ConvertNumMode)
	putInt(params, "word_info", config.WordInfo)

	// 密钥独立存储，后续可做脱敏处理
	secrets := map[string]string{}
	putString(secrets, "secretId", config.SecretID)
	putString(secrets, "secretKey", config.SecretKey)

	return model.ProviderConfig{
		Provider: TencentASRProvider,
		Endpoint: endpoint,
		Params:   params,
		Secrets:  secrets,
	}
}

// putTencentASRInputSampleRate 只在腾讯 PCM 8k 上采样场景写入 input_sample_rate 参数。
// 其他格式（如 opus）不需要此参数，由 SDK 在连接时自动协商。
func putTencentASRInputSampleRate(values map[string]string, voiceFormat string, sampleRate int) {
	format := strings.ToLower(strings.TrimSpace(voiceFormat))
	if format != "pcm" && format != "1" {
		return
	}
	if sampleRate == 8000 {
		putInt(values, "input_sample_rate", sampleRate)
	}
}

// TencentTTSProviderConfig 将腾讯云 TTS 业务参数转换为 PBX provider 标准化配置。
func TencentTTSProviderConfig(config TencentTTSConfig) model.ProviderConfig {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://tts.tencentcloudapi.com"
	}
	params := map[string]string{}
	putString(params, "appId", config.AppID)
	putString(params, "voiceType", config.VoiceType)
	putString(params, "codec", config.Codec)
	putString(params, "language", config.Language)
	putInt(params, "sampleRate", config.SampleRate)
	if config.Speed != 0 {
		params["speed"] = strconv.FormatFloat(config.Speed, 'f', -1, 64)
	}
	if config.Volume != 0 {
		params["volume"] = strconv.FormatFloat(config.Volume, 'f', -1, 64)
	}

	secrets := map[string]string{}
	putString(secrets, "secretId", config.SecretID)
	putString(secrets, "secretKey", config.SecretKey)

	return model.ProviderConfig{
		Provider: TencentTTSProvider,
		Endpoint: endpoint,
		Params:   params,
		Secrets:  secrets,
	}
}

// cloneProviderConfig 深拷贝 provider 配置，防止外部修改 SDK 内部状态。
func cloneProviderConfig(config model.ProviderConfig) model.ProviderConfig {
	return model.ProviderConfig{
		Provider: config.Provider,
		Endpoint: config.Endpoint,
		Params:   cloneStringMap(config.Params),
		Secrets:  cloneStringMap(config.Secrets),
	}
}

// cloneProviderConfigMap 深拷贝 provider 配置集合。
func cloneProviderConfigMap(configs map[model.CapabilityType]model.ProviderConfig) map[model.CapabilityType]model.ProviderConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make(map[model.CapabilityType]model.ProviderConfig, len(configs))
	for typ, config := range configs {
		out[typ] = cloneProviderConfig(config)
	}
	return out
}

// cloneStringMap 深拷贝字符串 map，防止共享底层结构。
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

// putString 仅写入非空字符串参数，避免传递空值给 provider。
func putString(values map[string]string, key, value string) {
	if value != "" {
		values[key] = value
	}
}

// putInt 仅写入非零整数参数（零值表示"未设置"，不应覆盖 provider 默认值）。
func putInt(values map[string]string, key string, value int) {
	if value != 0 {
		values[key] = strconv.Itoa(value)
	}
}

// putBool 仅写入 true 的布尔参数（"1"），false 时不设置（依赖 provider 默认值）。
func putBool(values map[string]string, key string, value bool) {
	if value {
		values[key] = "1"
	}
}
