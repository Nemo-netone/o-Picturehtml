import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import {
  getUtteranceId,
  isAsrResult,
  isTranslationResult,
  isWsMessage,
  type WsMessage,
} from '../src/types/protocol'

const here = dirname(fileURLToPath(import.meta.url))
const fixture = JSON.parse(
  readFileSync(join(here, 'fixtures', 'one-utterance.json'), 'utf8'),
) as {
  utteranceId: string
  frames: { delayMs: number; frame: WsMessage }[]
}

const frames = fixture.frames.map((f) => f.frame)

describe('one-utterance fixture', () => {
  it('every frame is a valid protocol message', () => {
    for (const frame of frames) {
      expect(isWsMessage(frame)).toBe(true)
    }
  })

  it('all frames share the fixture utteranceId', () => {
    for (const frame of frames) {
      expect(getUtteranceId(frame)).toBe(fixture.utteranceId)
    }
  })

  it('covers asr partial → final', () => {
    const asr = frames.filter(isAsrResult)
    expect(asr.some((f) => !f.isFinal)).toBe(true)
    expect(asr.some((f) => f.isFinal)).toBe(true)
  })

  it('covers tmt partial (multi) → deepseek final, with a revised frame', () => {
    const tr = frames.filter(isTranslationResult)
    expect(tr.filter((f) => f.engine === 'tmt').length).toBeGreaterThanOrEqual(
      2,
    )
    const deepseek = tr.filter((f) => f.engine === 'deepseek-flash')
    expect(deepseek.some((f) => f.isFinal)).toBe(true)
    expect(tr.some((f) => f.revised)).toBe(true)
  })
})
