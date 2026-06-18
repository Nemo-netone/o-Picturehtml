import type { CSSProperties } from 'react'

import type { SubtitleLine } from '../types/protocol'

export type SubtitleStyleTarget = 'card' | 'en' | 'zh'

export type SubtitleStyle = CSSProperties & {
  '--subtitle-revise-highlight'?: string
  '--subtitle-final-color'?: string
  '--revise-highlight-ms'?: string
}

export const SUBTITLE_COLOR = {
  pending: '#6b7280',
  final: '#111827',
  reviseHighlight: '#dff7ff',
} as const

export const SUBTITLE_MOTION = {
  reviseHighlightMs: 600,
} as const

export const SUBTITLE_SCROLL = {
  bottomThresholdPx: 24,
} as const

export const SUBTITLE_TEXT = {
  panelTitle: '双语字幕',
  panelModeLabel: '按句分块',
  autoScrollLabel: '自动滚动',
  autoScrollPausedLabel: '上滚暂停',
  emptyTitle: '等待音频源',
  emptyHint: '选择音频源与策略后点「开始」，双语字幕将在此实时呈现',
  lockLabel: '🔒',
  revisedLabel: '✦已纠正',
} as const

function isFinal(line: SubtitleLine, target: SubtitleStyleTarget) {
  if (target === 'en') {
    return line.enFinal
  }

  return target === 'zh' && line.zhFinal
}

export function getSubtitleStyle(
  line: SubtitleLine,
  target: SubtitleStyleTarget,
): SubtitleStyle {
  if (target === 'card') {
    return {
      '--subtitle-revise-highlight': SUBTITLE_COLOR.reviseHighlight,
      '--subtitle-final-color': SUBTITLE_COLOR.final,
      '--revise-highlight-ms': `${SUBTITLE_MOTION.reviseHighlightMs}ms`,
    }
  }

  return {
    color: isFinal(line, target)
      ? SUBTITLE_COLOR.final
      : SUBTITLE_COLOR.pending,
  }
}
