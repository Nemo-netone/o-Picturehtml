package model

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	LanguageMetadataSourceLanguage     = "sourceLanguage"
	LanguageMetadataTargetLanguage     = "targetLanguage"
	LanguageMetadataASREngineType      = "asrEngineType"
	LanguageMetadataTTSLanguage        = "ttsLanguage"
	LanguageMetadataTTSPrimaryLanguage = "ttsPrimaryLanguage"
	LanguageMetadataTTSVoiceType       = "ttsVoiceType"
)

const (
	DefaultSourceLanguage = "en"
	DefaultTargetLanguage = "zh"
	DefaultASREngineType  = "16k_en"
	DefaultTTSLanguage    = "zh-CN"
	DefaultTTSVoiceType   = "101001"
)

type SessionLanguageOptions struct {
	SourceLanguage     string
	TargetLanguage     string
	ASREngineType      string
	TTSLanguage        string
	TTSPrimaryLanguage int
	TTSVoiceType       string
}

var asrEngineByLanguage = map[string]string{
	"zh":  "16k_zh",
	"en":  "16k_en",
	"id":  "16k_id",
	"fil": "16k_fil",
	"th":  "16k_th",
	"pt":  "16k_pt",
	"tr":  "16k_tr",
	"ar":  "16k_ar",
	"es":  "16k_es",
	"hi":  "16k_hi",
	"fr":  "16k_fr",
	"de":  "16k_de",
}

var languageByASREngine = map[string]string{
	"16k_zh":  "zh",
	"16k_en":  "en",
	"16k_id":  "id",
	"16k_fil": "fil",
	"16k_th":  "th",
	"16k_pt":  "pt",
	"16k_tr":  "tr",
	"16k_ar":  "ar",
	"16k_es":  "es",
	"16k_hi":  "hi",
	"16k_fr":  "fr",
	"16k_de":  "de",
}

func DefaultSessionLanguageOptions() SessionLanguageOptions {
	return SessionLanguageOptions{
		SourceLanguage:     DefaultSourceLanguage,
		TargetLanguage:     DefaultTargetLanguage,
		ASREngineType:      DefaultASREngineType,
		TTSLanguage:        DefaultTTSLanguage,
		TTSPrimaryLanguage: 1,
		TTSVoiceType:       DefaultTTSVoiceType,
	}
}

func NormalizeSessionLanguageOptions(metadata map[string]string) (SessionLanguageOptions, error) {
	options := DefaultSessionLanguageOptions()
	source := firstLanguageMetadata(metadata, LanguageMetadataSourceLanguage, "sourceLang", "source_language")
	target := firstLanguageMetadata(metadata, LanguageMetadataTargetLanguage, "targetLang", "target_language")
	engine := firstLanguageMetadata(metadata, LanguageMetadataASREngineType, "engine_model_type", "engineModelType", "asr_engine_type")
	voice := firstLanguageMetadata(metadata, LanguageMetadataTTSVoiceType, "voiceType", "tts_voice_type")

	if source != "" {
		normalized, err := NormalizeLanguageCode(source)
		if err != nil {
			return SessionLanguageOptions{}, err
		}
		options.SourceLanguage = normalized
	}
	if target != "" {
		normalized, err := NormalizeLanguageCode(target)
		if err != nil {
			return SessionLanguageOptions{}, err
		}
		options.TargetLanguage = normalized
	}
	if options.TargetLanguage != "zh" && options.TargetLanguage != "en" {
		return SessionLanguageOptions{}, fmt.Errorf("unsupported target language %q: only zh/en are supported", options.TargetLanguage)
	}
	if options.SourceLanguage == options.TargetLanguage {
		return SessionLanguageOptions{}, fmt.Errorf("sourceLanguage and targetLanguage must be different: %s", options.SourceLanguage)
	}

	options.ASREngineType = asrEngineByLanguage[options.SourceLanguage]
	if engine != "" {
		engine = strings.ToLower(strings.TrimSpace(engine))
		language, ok := languageByASREngine[engine]
		if !ok {
			return SessionLanguageOptions{}, fmt.Errorf("unsupported asrEngineType %q", engine)
		}
		if language != options.SourceLanguage {
			return SessionLanguageOptions{}, fmt.Errorf("asrEngineType %s does not match sourceLanguage %s", engine, options.SourceLanguage)
		}
		options.ASREngineType = engine
	}

	options.TTSLanguage, options.TTSPrimaryLanguage = ttsLanguageForTarget(options.TargetLanguage)
	if voice != "" {
		options.TTSVoiceType = strings.TrimSpace(voice)
	} else if options.TargetLanguage == "en" {
		options.TTSVoiceType = "101050"
	}
	return options, nil
}

func NormalizeLanguageCode(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "zh-cn", "zh-hans", "cn", "chinese":
		normalized = "zh"
	case "en-us", "en-gb", "english":
		normalized = "en"
	case "tl", "fil-ph", "filipino":
		normalized = "fil"
	case "id-id", "indonesian":
		normalized = "id"
	case "th-th", "thai":
		normalized = "th"
	case "pt-br", "pt-pt", "portuguese":
		normalized = "pt"
	case "tr-tr", "turkish":
		normalized = "tr"
	case "ar-sa", "arabic":
		normalized = "ar"
	case "es-es", "es-mx", "spanish":
		normalized = "es"
	case "hi-in", "hindi":
		normalized = "hi"
	case "fr-fr", "french":
		normalized = "fr"
	case "de-de", "german":
		normalized = "de"
	}
	if _, ok := asrEngineByLanguage[normalized]; !ok {
		return "", fmt.Errorf("unsupported source language %q", value)
	}
	return normalized, nil
}

func (o SessionLanguageOptions) WithDefaults() SessionLanguageOptions {
	if o.SourceLanguage == "" {
		o.SourceLanguage = DefaultSourceLanguage
	}
	if o.TargetLanguage == "" {
		o.TargetLanguage = DefaultTargetLanguage
	}
	if o.ASREngineType == "" {
		o.ASREngineType = asrEngineByLanguage[o.SourceLanguage]
	}
	if o.TTSLanguage == "" || o.TTSPrimaryLanguage == 0 {
		o.TTSLanguage, o.TTSPrimaryLanguage = ttsLanguageForTarget(o.TargetLanguage)
	}
	if o.TTSVoiceType == "" {
		o.TTSVoiceType = DefaultTTSVoiceType
		if o.TargetLanguage == "en" {
			o.TTSVoiceType = "101050"
		}
	}
	return o
}

func (o SessionLanguageOptions) Metadata() map[string]string {
	o = o.WithDefaults()
	return map[string]string{
		LanguageMetadataSourceLanguage:     o.SourceLanguage,
		LanguageMetadataTargetLanguage:     o.TargetLanguage,
		LanguageMetadataASREngineType:      o.ASREngineType,
		LanguageMetadataTTSLanguage:        o.TTSLanguage,
		LanguageMetadataTTSPrimaryLanguage: strconv.Itoa(o.TTSPrimaryLanguage),
		LanguageMetadataTTSVoiceType:       o.TTSVoiceType,
	}
}

func ApplySessionLanguageOptionsToProviderConfigs(configs map[CapabilityType]ProviderConfig, options SessionLanguageOptions) map[CapabilityType]ProviderConfig {
	if len(configs) == 0 {
		return nil
	}
	options = options.WithDefaults()
	out := CloneProviderConfigs(configs)
	if config, ok := out[CapabilityTypeASR]; ok {
		config.Params = ensureProviderParams(config.Params)
		config.Params["engine_model_type"] = options.ASREngineType
		config.Params["language"] = options.SourceLanguage
		out[CapabilityTypeASR] = config
	}
	if config, ok := out[CapabilityTypeTMT]; ok {
		config.Params = ensureProviderParams(config.Params)
		config.Params["source"] = options.SourceLanguage
		config.Params["target"] = options.TargetLanguage
		out[CapabilityTypeTMT] = config
	}
	if config, ok := out[CapabilityTypeTTS]; ok {
		config.Params = ensureProviderParams(config.Params)
		config.Params["language"] = options.TTSLanguage
		config.Params["primaryLanguage"] = strconv.Itoa(options.TTSPrimaryLanguage)
		config.Params["voiceType"] = options.TTSVoiceType
		out[CapabilityTypeTTS] = config
	}
	return out
}

func ttsLanguageForTarget(target string) (string, int) {
	if target == "en" {
		return "en-US", 2
	}
	return DefaultTTSLanguage, 1
}

func firstLanguageMetadata(metadata map[string]string, keys ...string) string {
	for _, key := range keys {
		if strings.TrimSpace(metadata[key]) != "" {
			return strings.TrimSpace(metadata[key])
		}
	}
	return ""
}

func ensureProviderParams(params map[string]string) map[string]string {
	if params == nil {
		return map[string]string{}
	}
	return params
}
