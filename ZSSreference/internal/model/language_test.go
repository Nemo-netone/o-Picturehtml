package model

import "testing"

func TestNormalizeSessionLanguageOptionsDefaults(t *testing.T) {
	options, err := NormalizeSessionLanguageOptions(nil)
	if err != nil {
		t.Fatalf("normalize defaults: %v", err)
	}
	if options.SourceLanguage != "en" || options.TargetLanguage != "zh" || options.ASREngineType != "16k_en" {
		t.Fatalf("unexpected default language options: %#v", options)
	}
	if options.TTSLanguage != "zh-CN" || options.TTSPrimaryLanguage != 1 || options.TTSVoiceType != "101001" {
		t.Fatalf("unexpected default tts options: %#v", options)
	}
}

func TestNormalizeSessionLanguageOptionsChineseToEnglish(t *testing.T) {
	options, err := NormalizeSessionLanguageOptions(map[string]string{
		"sourceLanguage": "zh-CN",
		"targetLanguage": "en",
	})
	if err != nil {
		t.Fatalf("normalize zh->en: %v", err)
	}
	if options.SourceLanguage != "zh" || options.TargetLanguage != "en" || options.ASREngineType != "16k_zh" {
		t.Fatalf("unexpected language options: %#v", options)
	}
	if options.TTSLanguage != "en-US" || options.TTSPrimaryLanguage != 2 || options.TTSVoiceType != "101050" {
		t.Fatalf("unexpected tts options: %#v", options)
	}
}

func TestNormalizeSessionLanguageOptionsRejectsInvalidPairs(t *testing.T) {
	for name, metadata := range map[string]map[string]string{
		"same language": {"sourceLanguage": "en", "targetLanguage": "en"},
		"bad target":    {"sourceLanguage": "en", "targetLanguage": "fr"},
		"bad engine":    {"sourceLanguage": "zh", "targetLanguage": "en", "asrEngineType": "16k_en"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeSessionLanguageOptions(metadata); err == nil {
				t.Fatalf("expected error for %#v", metadata)
			}
		})
	}
}

func TestApplySessionLanguageOptionsToProviderConfigs(t *testing.T) {
	options, err := NormalizeSessionLanguageOptions(map[string]string{
		"sourceLanguage": "zh",
		"targetLanguage": "en",
		"ttsVoiceType":   "101050",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	configs := ApplySessionLanguageOptionsToProviderConfigs(map[CapabilityType]ProviderConfig{
		CapabilityTypeASR: {Provider: "tencent-asr", Params: map[string]string{"forward_partial": "1"}},
		CapabilityTypeTMT: {Provider: "tencent-tmt"},
		CapabilityTypeTTS: {Provider: "tencent-tts"},
	}, options)
	if configs[CapabilityTypeASR].Params["engine_model_type"] != "16k_zh" || configs[CapabilityTypeASR].Params["language"] != "zh" {
		t.Fatalf("unexpected asr params: %#v", configs[CapabilityTypeASR].Params)
	}
	if configs[CapabilityTypeTMT].Params["source"] != "zh" || configs[CapabilityTypeTMT].Params["target"] != "en" {
		t.Fatalf("unexpected tmt params: %#v", configs[CapabilityTypeTMT].Params)
	}
	if configs[CapabilityTypeTTS].Params["language"] != "en-US" || configs[CapabilityTypeTTS].Params["primaryLanguage"] != "2" || configs[CapabilityTypeTTS].Params["voiceType"] != "101050" {
		t.Fatalf("unexpected tts params: %#v", configs[CapabilityTypeTTS].Params)
	}
}
