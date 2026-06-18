import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'

import type { SubtitleLine } from '../types/protocol'
import { useSubtitleStore } from '../state/subtitles'
import {
  SUBTITLE_SCROLL,
  SUBTITLE_TEXT,
  getSubtitleStyle,
} from '../styles/tokens'
import { shouldPauseAutoScroll } from './subtitleScroll'

function renderLockLabel(label: string) {
  return (
    <span className="subtitle-lock" aria-label={label} title={label}>
      {SUBTITLE_TEXT.lockLabel}
    </span>
  )
}

function SubtitleBlock({ line }: { line: SubtitleLine }) {
  const isRevisedFinal = line.zhFinal && line.revised

  return (
    <article
      className={[
        'subtitle-card',
        isRevisedFinal ? 'subtitle-card-revised' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      style={getSubtitleStyle(line, 'card')}
    >
      {line.en ? (
        <div className="subtitle-row subtitle-row-en">
          <span className="subtitle-language">EN</span>
          <p className="subtitle-text" style={getSubtitleStyle(line, 'en')}>
            {line.en}
          </p>
          <span className="subtitle-badges">
            {line.enFinal ? renderLockLabel('英文已锁定') : null}
          </span>
        </div>
      ) : null}

      {line.zh ? (
        <div className="subtitle-row subtitle-row-zh">
          <span className="subtitle-language">中</span>
          <p className="subtitle-text" style={getSubtitleStyle(line, 'zh')}>
            {line.zh}
          </p>
          <span className="subtitle-badges">
            {isRevisedFinal ? (
              <span className="subtitle-revised">
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

function getScrollKey(lines: SubtitleLine[]) {
  const lastLine = lines.at(-1)

  if (!lastLine) {
    return 'empty'
  }

  return [
    lines.length,
    lastLine.utteranceId,
    lastLine.en,
    lastLine.zh,
    String(lastLine.enFinal),
    String(lastLine.zhFinal),
    String(lastLine.revised),
  ].join('|')
}

export function SubtitlePanel() {
  const lines = useSubtitleStore((state) => state.lines)
  const visibleLines = lines.filter((line) => line.en || line.zh)
  const stageRef = useRef<HTMLDivElement | null>(null)
  const [isAutoScrollPaused, setIsAutoScrollPaused] = useState(false)
  const scrollKey = useMemo(() => getScrollKey(visibleLines), [visibleLines])

  const handleStageScroll = useCallback(() => {
    const stage = stageRef.current

    if (!stage) {
      return
    }

    const nextPaused = shouldPauseAutoScroll(
      {
        scrollTop: stage.scrollTop,
        scrollHeight: stage.scrollHeight,
        clientHeight: stage.clientHeight,
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

    const stage = stageRef.current

    if (!stage) {
      return
    }

    stage.scrollTop = stage.scrollHeight
  }, [isAutoScrollPaused, scrollKey])

  return (
    <section className="subtitle-panel" aria-label={SUBTITLE_TEXT.panelTitle}>
      <div className="panel-inner">
        <div className="panel-heading">
          <h2>{SUBTITLE_TEXT.panelTitle}</h2>
          <span>
            {isAutoScrollPaused
              ? SUBTITLE_TEXT.autoScrollPausedLabel
              : SUBTITLE_TEXT.autoScrollLabel}
          </span>
        </div>

        <div
          ref={stageRef}
          className="subtitle-stage"
          onScroll={handleStageScroll}
        >
          {visibleLines.length > 0 ? (
            <div className="subtitle-list" aria-live="polite">
              {visibleLines.map((line) => (
                <SubtitleBlock key={line.utteranceId} line={line} />
              ))}
            </div>
          ) : (
            <div className="subtitle-empty">
              <p className="subtitle-empty-title">{SUBTITLE_TEXT.emptyTitle}</p>
              <p className="subtitle-empty-hint">{SUBTITLE_TEXT.emptyHint}</p>
            </div>
          )}
        </div>
      </div>
    </section>
  )
}
