import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'

import { CustomSelect } from './CustomSelect'
import {
  AUDIO_SOURCE_OPTIONS,
  STRATEGY_OPTIONS,
} from './controlOptions'
import {
  getPiPScrollKey,
  getVisiblePiPLines,
} from './subtitlePiP'
import { shouldPauseAutoScroll } from './subtitleScroll'
import type { SessionAudioSource } from '../session/useSession'
import {
  SUBTITLE_SCROLL,
  SUBTITLE_TEXT,
  getSubtitleStyle,
} from '../styles/tokens'
import type { Strategy, SubtitleLine } from '../types/protocol'

export type SubtitlePiPSessionStatus = 'idle' | 'connecting' | 'connected' | 'disconnected'

export type SubtitlePiPControls = {
  audioSource: SessionAudioSource
  strategy: Strategy
  dubbing: boolean
  isStarting: boolean
  isRunning: boolean
  isStopping: boolean
  isStrategyPending: boolean
  errorMessage?: string
  promptMessage?: string
  onAudioSourceChange: (source: SessionAudioSource) => void
  onStrategyChange: (strategy: Strategy) => void
  onDubbingChange: (enabled: boolean) => void
  onStart: () => void
  onStop: () => void
}

type SubtitlePiPPanelProps = {
  lines: SubtitleLine[]
  sessionStatus: SubtitlePiPSessionStatus
  controls: SubtitlePiPControls
}

const STATUS_LABEL: Record<SubtitlePiPSessionStatus, string> = {
  idle: '未连接',
  connecting: '连接中',
  connected: '已连接',
  disconnected: '已断开',
}

function renderLockLabel(label: string) {
  return (
    <span className="subtitle-pip-lock" aria-label={label} title={label}>
      {SUBTITLE_TEXT.lockLabel}
    </span>
  )
}

function SubtitlePiPBlock({
  line,
  isCurrent,
}: {
  line: SubtitleLine
  isCurrent: boolean
}) {
  const isRevisedFinal = line.zhFinal && line.revised

  return (
    <article
      className={[
        'subtitle-pip-card',
        isRevisedFinal ? 'subtitle-pip-card-revised' : '',
        isCurrent ? 'subtitle-pip-card-current' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      style={getSubtitleStyle(line, 'card') as CSSProperties}
    >
      {line.en ? (
        <div className="subtitle-pip-row subtitle-pip-row-en">
          <span className="subtitle-pip-language">EN</span>
          <p
            className="subtitle-pip-text"
            style={getSubtitleStyle(line, 'en') as CSSProperties}
          >
            {line.en}
          </p>
          <span className="subtitle-pip-badges">
            {line.enFinal ? renderLockLabel('英文已锁定') : null}
          </span>
        </div>
      ) : null}

      {line.zh ? (
        <div className="subtitle-pip-row subtitle-pip-row-zh">
          <span className="subtitle-pip-language">中</span>
          <p
            className="subtitle-pip-text"
            style={getSubtitleStyle(line, 'zh') as CSSProperties}
          >
            {line.zh}
          </p>
          <span className="subtitle-pip-badges">
            {isRevisedFinal ? (
              <span className="subtitle-pip-revised">
                {SUBTITLE_TEXT.revisedLabel}
              </span>
            ) : null}
            {line.zhFinal ? renderLockLabel('中文已锁定') : null}
          </span>
        </div>
      ) : null}
    </article>
  )
}

function SubtitlePiPControlsStrip({
  audioSource,
  strategy,
  dubbing,
  isStarting,
  isRunning,
  isStopping,
  isStrategyPending,
  sessionStatus,
  onAudioSourceChange,
  onStrategyChange,
  onDubbingChange,
  onStart,
  onStop,
}: SubtitlePiPControls & { sessionStatus: SubtitlePiPSessionStatus }) {
  const isBusy = isStarting || isStopping
  const lockSessionOptions = isBusy || isRunning
  const shouldStop = isStarting || isRunning
  const sessionActionLabel = isStopping ? '停止中' : shouldStop ? '停止' : '开始'
  return (
    <footer className="subtitle-pip-footer" aria-label="小窗同传控制">
      <div className="subtitle-pip-footer-main">
        <div className="subtitle-pip-field">
          <CustomSelect
            value={audioSource}
            options={AUDIO_SOURCE_OPTIONS}
            disabled={lockSessionOptions}
            label="音频源"
            onChange={onAudioSourceChange}
          />
        </div>

        <div className="subtitle-pip-field">
          <CustomSelect
            value={strategy}
            options={STRATEGY_OPTIONS}
            disabled={isBusy || isStrategyPending}
            label="策略"
            onChange={onStrategyChange}
          />
        </div>

        <label
          className="subtitle-pip-switch-field"
          data-enabled={dubbing ? 'true' : 'false'}
          aria-checked={dubbing}
        >
          <input
            type="checkbox"
            checked={dubbing}
            disabled={lockSessionOptions}
            onChange={(event) => onDubbingChange(event.target.checked)}
          />
          <span className="subtitle-pip-switch-indicator" aria-hidden="true" />
          <span>配音</span>
        </label>
      </div>

      <span
        className={`subtitle-pip-chip subtitle-pip-connection-status subtitle-pip-connection-status-${sessionStatus}`}
      >
        {STATUS_LABEL[sessionStatus]}
      </span>

      <button
        type="button"
        className={[
          'subtitle-pip-chip',
          'subtitle-pip-footer-action',
          shouldStop ? 'subtitle-pip-footer-action-stop' : '',
        ]
          .filter(Boolean)
          .join(' ')}
        disabled={isStopping}
        onClick={shouldStop ? onStop : onStart}
      >
        {sessionActionLabel}
      </button>
    </footer>
  )
}

export function SubtitlePiPPanel({
  lines,
  sessionStatus,
  controls,
}: SubtitlePiPPanelProps) {
  const visibleLines = useMemo(() => getVisiblePiPLines(lines), [lines])
  const bodyRef = useRef<HTMLDivElement | null>(null)
  const [isAutoScrollPaused, setIsAutoScrollPaused] = useState(false)
  const scrollKey = useMemo(() => getPiPScrollKey(visibleLines), [visibleLines])

  const handleBodyScroll = useCallback(() => {
    const body = bodyRef.current

    if (!body) {
      return
    }

    const nextPaused = shouldPauseAutoScroll(
      {
        scrollTop: body.scrollTop,
        scrollHeight: body.scrollHeight,
        clientHeight: body.clientHeight,
      },
      SUBTITLE_SCROLL.bottomThresholdPx,
    )

    setIsAutoScrollPaused((current) =>
      current === nextPaused ? current : nextPaused,
    )
  }, [])

  useLayoutEffect(() => {
    if (isAutoScrollPaused) {
      return
    }

    const body = bodyRef.current

    if (!body) {
      return
    }

    body.scrollTop = body.scrollHeight
  }, [isAutoScrollPaused, scrollKey])

  return (
    <section className="subtitle-pip" aria-label="悬浮双语字幕">
      <main
        ref={bodyRef}
        className="subtitle-pip-body"
        aria-live="polite"
        onScroll={handleBodyScroll}
      >
        {visibleLines.length > 0 ? (
          <div className="subtitle-pip-list">
            {visibleLines.map((line, index) => (
              <SubtitlePiPBlock
                key={line.utteranceId}
                line={line}
                isCurrent={index === visibleLines.length - 1}
              />
            ))}
          </div>
        ) : (
          <div className="subtitle-pip-empty">
            <p className="subtitle-pip-empty-title">等待字幕...</p>
            <p className="subtitle-pip-empty-hint">
              开始同传或播放 mock 回放后，英文原文和中文译文会同步显示在这里。
            </p>
          </div>
        )}
      </main>

      <SubtitlePiPControlsStrip {...controls} sessionStatus={sessionStatus} />
    </section>
  )
}
