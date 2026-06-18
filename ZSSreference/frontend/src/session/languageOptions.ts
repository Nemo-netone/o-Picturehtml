import type {
  AsrEngineType,
  SourceLanguage,
  TargetLanguage,
  TtsVoiceType,
} from '../types/protocol'

export const ASR_ENGINE_BY_SOURCE_LANGUAGE: Record<
  SourceLanguage,
  AsrEngineType
> = {
  zh: '16k_zh',
  en: '16k_en',
  id: '16k_id',
  fil: '16k_fil',
  th: '16k_th',
  pt: '16k_pt',
  tr: '16k_tr',
  ar: '16k_ar',
  es: '16k_es',
  hi: '16k_hi',
  fr: '16k_fr',
  de: '16k_de',
}

export const DEFAULT_TTS_VOICE_BY_TARGET_LANGUAGE: Record<
  TargetLanguage,
  TtsVoiceType
> = {
  zh: '101001',
  en: '101050',
}
