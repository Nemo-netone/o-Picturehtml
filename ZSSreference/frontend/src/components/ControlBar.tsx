import { CustomSelect } from './CustomSelect'
import {
  AUDIO_SOURCE_OPTIONS,
  SOURCE_LANGUAGE_OPTIONS,
  STRATEGY_OPTIONS,
  TARGET_LANGUAGE_OPTIONS,
} from './controlOptions'
import type { SessionAudioSource } from '../session/useSession'
import type { SourceLanguage, Strategy, TargetLanguage } from '../types/protocol'
import { AudioVisualizer } from './AudioVisualizer'

type ControlBarProps = {
  audioSource: SessionAudioSource
  strategy: Strategy
  dubbing: boolean
  sourceLanguage: SourceLanguage
  targetLanguage: TargetLanguage
  isStarting: boolean
  isRunning: boolean
  isStopping: boolean
  isStrategyPending: boolean
  audioLevel: number
  errorMessage?: string
  promptMessage?: string
  onAudioSourceChange: (source: SessionAudioSource) => void
  onSourceLanguageChange: (language: SourceLanguage) => void
  onTargetLanguageChange: (language: TargetLanguage) => void
  onStrategyChange: (strategy: Strategy) => void
  onDubbingChange: (enabled: boolean) => void
  onStart: () => void
  onStop: () => void
}

export function ControlBar({
  audioSource,
  strategy,
  dubbing,
  sourceLanguage,
  targetLanguage,
  isStarting,
  isRunning,
  isStopping,
  isStrategyPending,
  audioLevel,
  errorMessage,
  promptMessage,
  onAudioSourceChange,
  onSourceLanguageChange,
  onTargetLanguageChange,
  onStrategyChange,
  onDubbingChange,
  onStart,
  onStop,
}: ControlBarProps) {
  const isBusy = isStarting || isStopping
  const lockSessionOptions = isBusy || isRunning
  const shouldStop = isStarting || isRunning
  const actionLabel = isStopping ? '停止中' : shouldStop ? '停止' : '开始'

  return (
    <section className="control-bar" aria-label="同传控制">
      <div className="bar-inner">
        <div className="control-group">
          <div className="field">
            <span>输入语言</span>
            <CustomSelect
              value={sourceLanguage}
              options={SOURCE_LANGUAGE_OPTIONS}
              disabled={lockSessionOptions}
              label="输入语言"
              onChange={onSourceLanguageChange}
            />
          </div>

          <div className="field">
            <span>输出语言</span>
            <CustomSelect
              value={targetLanguage}
              options={TARGET_LANGUAGE_OPTIONS}
              disabled={lockSessionOptions}
              label="输出语言"
              onChange={onTargetLanguageChange}
            />
          </div>

          <div className="field">
            <span>音频源</span>
            <CustomSelect
              value={audioSource}
              options={AUDIO_SOURCE_OPTIONS}
              disabled={lockSessionOptions}
              label="音频源"
              onChange={onAudioSourceChange}
            />
          </div>

          <div className="field">
            <span>策略</span>
            <CustomSelect
              value={strategy}
              options={STRATEGY_OPTIONS}
              disabled={isBusy || isStrategyPending}
              label="策略"
              onChange={onStrategyChange}
            />
          </div>

          <label className="switch-field">
            <span>配音</span>
            <input
              type="checkbox"
              checked={dubbing}
              onChange={(event) => onDubbingChange(event.target.checked)}
              disabled={lockSessionOptions}
            />
          </label>

          <AudioVisualizer level={audioLevel} />
        </div>

        <div className="action-group">
          <button
            type="button"
            className={shouldStop ? 'secondary-action' : 'primary-action'}
            onClick={shouldStop ? onStop : onStart}
            disabled={isStopping}
          >
            <span
              className={
                shouldStop
                  ? 'button-icon button-icon-stop'
                  : 'button-icon button-icon-play'
              }
              aria-hidden="true"
            />
            {actionLabel}
          </button>
        </div>
        {promptMessage || errorMessage || isStrategyPending ? (
          <div
            className="control-message"
            role={errorMessage ? 'alert' : 'status'}
          >
            {errorMessage ?? promptMessage ?? '正在切换策略…'}
          </div>
        ) : null}
      </div>
    </section>
  )
}
