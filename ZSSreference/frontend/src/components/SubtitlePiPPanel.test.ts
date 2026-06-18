import { describe, expect, it } from 'vitest'

import {
  PIP_MAX_VISIBLE_LINES,
  getVisiblePiPLines,
} from './subtitlePiP'
import type { SubtitleLine } from '../types/protocol'

function subtitleLine(
  utteranceId: string,
  overrides: Partial<SubtitleLine> = {},
): SubtitleLine {
  return {
    utteranceId,
    en: '',
    enFinal: false,
    zh: '',
    zhFinal: false,
    revised: false,
    seq: Number.parseInt(utteranceId.replace(/\D/g, ''), 10) || 0,
    ...overrides,
  }
}

describe('getVisiblePiPLines', () => {
  it('keeps the latest eight non-empty subtitle lines in order', () => {
    const lines = [
      subtitleLine('empty-start'),
      ...Array.from({ length: PIP_MAX_VISIBLE_LINES + 2 }, (_, index) =>
        subtitleLine(`utt-${index}`, { en: `English ${index}`, seq: index }),
      ),
      subtitleLine('empty-end', { seq: 99 }),
    ]

    expect(getVisiblePiPLines(lines).map((line) => line.utteranceId)).toEqual([
      'utt-2',
      'utt-3',
      'utt-4',
      'utt-5',
      'utt-6',
      'utt-7',
      'utt-8',
      'utt-9',
    ])
  })

  it('preserves Chinese-only lines and final/revised metadata', () => {
    const line = subtitleLine('utt-zh-only', {
      zh: '只有中文译文',
      zhFinal: true,
      revised: true,
      engine: 'llm-tmt',
    })

    expect(getVisiblePiPLines([line])).toEqual([line])
  })
})
