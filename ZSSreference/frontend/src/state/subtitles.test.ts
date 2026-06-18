import { describe, expect, it } from 'vitest'

import { MAX_SUBTITLE_LINES, THROTTLE_MS } from '../config'
import {
  WS_TYPE,
  type AsrFinalMessage,
  type AsrResultMessage,
  type ConnectedMessage,
  type LlmTmtFinalMessage,
  type TmtFinalMessage,
  type TranslationResultMessage,
  type WsMessage,
} from '../types/protocol'
import {
  applyAsrFinal,
  applyAsrResult,
  applyLlmTmtFinal,
  applyTmtFinal,
  applyTranslationResult,
  createInitialSubtitleState,
  reduceSubtitleMessage,
  useSubtitleStore,
} from './subtitles'

const asrFrame = (
  utteranceId: string,
  text: string,
  isFinal: boolean,
): AsrResultMessage => ({
  type: WS_TYPE.asr_result,
  callId: 'call-1',
  userId: 'user-1',
  text,
  isFinal,
  metadata: { utteranceId },
})

const asrFinalFrame = (utteranceId: string, text: string): AsrFinalMessage => ({
  type: WS_TYPE.asr_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId,
  text,
  language: 'en',
})

const translationFrame = (
  utteranceId: string,
  text: string,
  options: Partial<
    Pick<TranslationResultMessage, 'engine' | 'isFinal' | 'revised'>
  > = {},
): TranslationResultMessage => ({
  type: WS_TYPE.translation_result,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId,
  sourceText: 'source text',
  text,
  isFinal: options.isFinal ?? false,
  engine: options.engine ?? 'tmt',
  revised: options.revised ?? false,
  language: 'zh',
})

const tmtFinalFrame = (utteranceId: string, text: string): TmtFinalMessage => ({
  type: WS_TYPE.tmt_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId,
  sourceText: 'source text',
  text,
  language: 'zh',
})

const llmTmtFinalFrame = (
  utteranceId: string,
  text: string,
  revised?: boolean,
): LlmTmtFinalMessage => ({
  type: WS_TYPE.llm_tmt_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId,
  sourceText: 'source text',
  text,
  revised,
  language: 'zh',
})

const fixture = {
  utteranceId: 'call-1-utt-7',
  frames: [
    {
      delayMs: 0,
      frame: asrFinalFrame(
        'call-1-utt-7',
        'We use a transformer architecture.',
      ),
    },
    {
      delayMs: 600,
      frame: tmtFinalFrame('call-1-utt-7', '我们使用变压器架构。'),
    },
    {
      delayMs: 1200,
      frame: llmTmtFinalFrame(
        'call-1-utt-7',
        '我们采用一种 Transformer 架构。',
        true,
      ),
    },
    {
      delayMs: 900,
      frame: asrFinalFrame(
        'call-1-utt-8',
        'The attention mechanism allows the model to focus on relevant tokens.',
      ),
    },
    {
      delayMs: 600,
      frame: tmtFinalFrame(
        'call-1-utt-8',
        '注意力机制允许模型关注相关 token。',
      ),
    },
    {
      delayMs: 1000,
      frame: llmTmtFinalFrame(
        'call-1-utt-8',
        '注意力机制允许模型关注相关 token。',
        false,
      ),
    },
    {
      delayMs: 900,
      frame: asrFinalFrame(
        'call-1-utt-9',
        'If the AI correction is delayed, the draft translation remains visible.',
      ),
    },
    {
      delayMs: 700,
      frame: tmtFinalFrame(
        'call-1-utt-9',
        '如果 AI 矫正延迟，草稿翻译仍然可见。',
      ),
    },
    {
      delayMs: 1000,
      frame: asrFinalFrame(
        'call-1-utt-10',
        'Sometimes the AI result can arrive without a TMT draft.',
      ),
    },
    {
      delayMs: 1100,
      frame: llmTmtFinalFrame(
        'call-1-utt-10',
        '有时 AI 结果会在没有 TMT 草稿的情况下直接到达。',
        false,
      ),
    },
    {
      delayMs: 900,
      frame: asrFinalFrame(
        'call-1-utt-11',
        'Late TMT packets should not roll back the corrected translation.',
      ),
    },
    {
      delayMs: 700,
      frame: tmtFinalFrame(
        'call-1-utt-11',
        '迟到的 TMT 包不应回滚更正后的翻译。',
      ),
    },
    {
      delayMs: 1000,
      frame: llmTmtFinalFrame(
        'call-1-utt-11',
        '迟到的 TMT 数据包不应让已矫正译文回退。',
        true,
      ),
    },
    {
      delayMs: 900,
      frame: tmtFinalFrame(
        'call-1-utt-11',
        '【迟到 TMT】不应该看到这句覆盖 AI 译文。',
      ),
    },
  ],
} satisfies {
  utteranceId: string
  frames: { delayMs: number; frame: WsMessage }[]
}

describe('reduceSubtitleMessage', () => {
  it('replays the final-flow fixture to a locked revised line', () => {
    let state = createInitialSubtitleState()
    let now = 0

    for (const { delayMs, frame } of fixture.frames) {
      now += delayMs
      state = reduceSubtitleMessage(state, frame, now)
    }

    expect(state.lines).toHaveLength(5)
    expect(state.byId.size).toBe(5)

    expect(state.byId.get('call-1-utt-7')).toMatchObject({
      utteranceId: 'call-1-utt-7',
      en: 'We use a transformer architecture.',
      enFinal: true,
      zh: '我们采用一种 Transformer 架构。',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: true,
      seq: 0,
    })
    expect(state.byId.get('call-1-utt-8')).toMatchObject({
      zh: '注意力机制允许模型关注相关 token。',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: false,
    })
    expect(state.byId.get('call-1-utt-9')).toMatchObject({
      zh: '如果 AI 矫正延迟，草稿翻译仍然可见。',
      zhFinal: false,
      engine: 'tmt',
      revised: false,
    })
    expect(state.byId.get('call-1-utt-10')).toMatchObject({
      zh: '有时 AI 结果会在没有 TMT 草稿的情况下直接到达。',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: false,
    })
    expect(state.byId.get('call-1-utt-11')).toMatchObject({
      zh: '迟到的 TMT 数据包不应让已矫正译文回退。',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: true,
    })
  })

  it('keeps accepting the legacy asr/translation flow during migration', () => {
    let state = createInitialSubtitleState()

    state = reduceSubtitleMessage(
      state,
      asrFrame('legacy-utt', 'We use a transformer architecture.', true),
    )
    state = reduceSubtitleMessage(
      state,
      translationFrame('legacy-utt', '我们采用一种 Transformer 架构。', {
        engine: 'deepseek-flash',
        isFinal: true,
        revised: true,
      }),
      0,
    )

    expect(state.lines[0]).toMatchObject({
      utteranceId: 'legacy-utt',
      enFinal: true,
      zhFinal: true,
      engine: 'deepseek-flash',
      revised: true,
    })
  })

  it('ignores non-subtitle protocol messages', () => {
    const state = createInitialSubtitleState()
    const connected: ConnectedMessage = {
      type: WS_TYPE.connected,
      connectionId: 'conn-1',
      tenantId: 'tenant-a',
      clientId: 'client-a',
    }

    expect(reduceSubtitleMessage(state, connected)).toBe(state)
  })

  it('ignores malformed messages without a usable utteranceId', () => {
    const state = createInitialSubtitleState()
    const malformed = {
      ...asrFrame('utt-1', 'hello', false),
      metadata: undefined,
    } as unknown as AsrResultMessage

    expect(applyAsrResult(state, malformed)).toBe(state)
  })
})

describe('applyAsrFinal', () => {
  it('creates and locks an English line', () => {
    const state = applyAsrFinal(
      createInitialSubtitleState(),
      asrFinalFrame('utt-1', 'Final sentence.'),
    )

    expect(state.lines[0]).toMatchObject({
      utteranceId: 'utt-1',
      en: 'Final sentence.',
      enFinal: true,
    })
  })
})

describe('applyAsrResult', () => {
  it('creates and updates English partials before final lock', () => {
    let state = createInitialSubtitleState()

    state = applyAsrResult(state, asrFrame('utt-1', 'We use', false))
    expect(state.lines).toHaveLength(1)
    expect(state.lines[0]).toMatchObject({
      utteranceId: 'utt-1',
      en: 'We use',
      enFinal: false,
    })

    state = applyAsrResult(state, asrFrame('utt-1', 'We use models', false))
    expect(state.lines[0].en).toBe('We use models')
    expect(state.lines[0].enFinal).toBe(false)

    state = applyAsrResult(state, asrFrame('utt-1', 'We use models.', true))
    expect(state.lines[0].en).toBe('We use models.')
    expect(state.lines[0].enFinal).toBe(true)
  })

  it('does not let a late ASR partial overwrite locked English', () => {
    let state = createInitialSubtitleState()

    state = applyAsrResult(state, asrFrame('utt-1', 'Final sentence.', true))
    const next = applyAsrResult(state, asrFrame('utt-1', 'Final', false))

    expect(next).toBe(state)
    expect(next.lines[0]).toMatchObject({
      en: 'Final sentence.',
      enFinal: true,
    })
  })
})

describe('applyTmtFinal', () => {
  it('creates or updates a gray TMT draft line', () => {
    const state = applyTmtFinal(
      createInitialSubtitleState(),
      tmtFinalFrame('utt-1', '我们使用变压器架构。'),
    )

    expect(state.lines[0]).toMatchObject({
      utteranceId: 'utt-1',
      en: '',
      zh: '我们使用变压器架构。',
      zhFinal: false,
      engine: 'tmt',
      revised: false,
    })
  })

  it('does not overwrite an AI-corrected final with late TMT', () => {
    let state = createInitialSubtitleState()
    state = applyLlmTmtFinal(
      state,
      llmTmtFinalFrame('utt-1', '我们采用一种 Transformer 架构。', true),
    )

    const next = applyTmtFinal(state, tmtFinalFrame('utt-1', '迟到的机器翻译'))

    expect(next).toBe(state)
    expect(next.lines[0]).toMatchObject({
      zh: '我们采用一种 Transformer 架构。',
      zhFinal: true,
      engine: 'llm-tmt',
    })
  })
})

describe('applyLlmTmtFinal', () => {
  it('replaces the TMT draft and locks Chinese', () => {
    let state = createInitialSubtitleState()
    state = applyTmtFinal(state, tmtFinalFrame('utt-1', '我们使用变压器架构。'))
    state = applyLlmTmtFinal(
      state,
      llmTmtFinalFrame('utt-1', '我们采用一种 Transformer 架构。', true),
    )

    expect(state.lines[0]).toMatchObject({
      zh: '我们采用一种 Transformer 架构。',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: true,
    })
  })

  it('derives revised when the backend omits the field', () => {
    let state = createInitialSubtitleState()
    state = applyTmtFinal(state, tmtFinalFrame('utt-1', '机器翻译'))
    state = applyLlmTmtFinal(state, llmTmtFinalFrame('utt-1', '人工智能矫正'))

    expect(state.lines[0].revised).toBe(true)
  })

  it('can create a Chinese-only line when TMT is missing', () => {
    const state = applyLlmTmtFinal(
      createInitialSubtitleState(),
      llmTmtFinalFrame('utt-zh-only', '只有 AI 译文', false),
    )

    expect(state.lines[0]).toMatchObject({
      utteranceId: 'utt-zh-only',
      en: '',
      zh: '只有 AI 译文',
      zhFinal: true,
      engine: 'llm-tmt',
      revised: false,
    })
  })
})

describe('applyTranslationResult', () => {
  it('throttles TMT partial updates by utterance', () => {
    let state = createInitialSubtitleState()

    state = applyTranslationResult(
      state,
      translationFrame('utt-1', '我们使用'),
      0,
    )
    expect(state.lines[0]).toMatchObject({
      zh: '我们使用',
      zhFinal: false,
      engine: 'tmt',
      revised: false,
    })

    state = applyTranslationResult(
      state,
      translationFrame('utt-1', '我们使用模型'),
      THROTTLE_MS - 1,
    )
    expect(state.lines[0].zh).toBe('我们使用')

    state = applyTranslationResult(
      state,
      translationFrame('utt-1', '我们使用模型'),
      THROTTLE_MS,
    )
    expect(state.lines[0].zh).toBe('我们使用模型')
  })

  it('creates a translation-only line for single-language fallback', () => {
    const state = applyTranslationResult(
      createInitialSubtitleState(),
      translationFrame('utt-zh-only', '只有中文'),
      0,
    )

    expect(state.lines).toHaveLength(1)
    expect(state.lines[0]).toMatchObject({
      utteranceId: 'utt-zh-only',
      en: '',
      enFinal: false,
      zh: '只有中文',
      zhFinal: false,
    })
  })

  it('locks DeepSeek final and ignores later TMT partials', () => {
    let state = createInitialSubtitleState()

    state = applyTranslationResult(
      state,
      translationFrame('utt-1', '机器翻译'),
      0,
    )
    state = applyTranslationResult(
      state,
      translationFrame('utt-1', '经过纠正的翻译。', {
        engine: 'deepseek-flash',
        isFinal: true,
        revised: true,
      }),
      10,
    )

    expect(state.lines[0]).toMatchObject({
      zh: '经过纠正的翻译。',
      zhFinal: true,
      engine: 'deepseek-flash',
      revised: true,
    })

    const next = applyTranslationResult(
      state,
      translationFrame('utt-1', '迟到的机器翻译'),
      THROTTLE_MS + 10,
    )

    expect(next).toBe(state)
    expect(next.lines[0]).toMatchObject({
      zh: '经过纠正的翻译。',
      zhFinal: true,
      engine: 'deepseek-flash',
      revised: true,
    })
  })
})

describe('subtitle retention', () => {
  it('keeps only the latest MAX_SUBTITLE_LINES and clears stale indexes', () => {
    let state = createInitialSubtitleState()

    for (let index = 0; index < MAX_SUBTITLE_LINES + 1; index += 1) {
      state = applyTranslationResult(
        state,
        translationFrame(`utt-${index}`, `译文 ${index}`),
        index * THROTTLE_MS,
      )
    }

    expect(state.lines).toHaveLength(MAX_SUBTITLE_LINES)
    expect(state.byId.size).toBe(MAX_SUBTITLE_LINES)
    expect(state.byId.has('utt-0')).toBe(false)
    expect(state.lastTmtPartialAtById.has('utt-0')).toBe(false)
    expect(state.byId.has(`utt-${MAX_SUBTITLE_LINES}`)).toBe(true)
    expect(state.lines.map((line) => line.seq)).toEqual(
      [...state.lines].map((line) => line.seq).sort((a, b) => a - b),
    )
  })
})

describe('useSubtitleStore', () => {
  it('dispatches messages, notifies subscribers, and resets state', () => {
    const seen: number[] = []
    const unsubscribe = useSubtitleStore.subscribe((state) => {
      seen.push(state.lines.length)
    })

    useSubtitleStore.getState().reset()
    useSubtitleStore
      .getState()
      .dispatch(asrFinalFrame('utt-store', 'Store line'))

    expect(useSubtitleStore.getState().lines[0]).toMatchObject({
      utteranceId: 'utt-store',
      en: 'Store line',
      enFinal: true,
    })
    expect(seen).toContain(1)

    unsubscribe()
    useSubtitleStore.getState().reset()
    expect(useSubtitleStore.getState().lines).toHaveLength(0)
  })
})
