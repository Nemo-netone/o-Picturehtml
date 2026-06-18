import type { SessionAudioSource } from '../session/useSession'
import type { SourceLanguage, Strategy, TargetLanguage } from '../types/protocol'

export type SelectOption<T extends string> = {
  value: T
  label: string
}

export const AUDIO_SOURCE_OPTIONS: SelectOption<SessionAudioSource>[] = [
  { value: 'microphone', label: '麦克风' },
  { value: 'tab', label: '标签页/系统' },
]

export const STRATEGY_OPTIONS: SelectOption<Strategy>[] = [
  { value: 'hybrid', label: '混合' },
  { value: 'tmt', label: '纯 TMT' },
  { value: 'deepseek', label: '纯 AI 矫正' },
]

export const SOURCE_LANGUAGE_OPTIONS: SelectOption<SourceLanguage>[] = [
  { value: 'en', label: '英语' },
  { value: 'zh', label: '中文' },
  { value: 'fr', label: '法语' },
  { value: 'de', label: '德语' },
  { value: 'es', label: '西班牙语' },
  { value: 'pt', label: '葡萄牙语' },
  { value: 'tr', label: '土耳其语' },
  { value: 'ar', label: '阿拉伯语' },
  { value: 'hi', label: '印地语' },
  { value: 'id', label: '印尼语' },
  { value: 'fil', label: '菲律宾语' },
  { value: 'th', label: '泰语' },
]

export const TARGET_LANGUAGE_OPTIONS: SelectOption<TargetLanguage>[] = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: '英语' },
]

export function isAudioSource(value: string): value is SessionAudioSource {
  return AUDIO_SOURCE_OPTIONS.some((option) => option.value === value)
}

export function isStrategy(value: string): value is Strategy {
  return STRATEGY_OPTIONS.some((option) => option.value === value)
}

export function isSourceLanguage(value: string): value is SourceLanguage {
  return SOURCE_LANGUAGE_OPTIONS.some((option) => option.value === value)
}

export function isTargetLanguage(value: string): value is TargetLanguage {
  return TARGET_LANGUAGE_OPTIONS.some((option) => option.value === value)
}
