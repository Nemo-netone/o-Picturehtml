import { describe, expect, it } from 'vitest'

import {
  getUtteranceId,
  isAsrFinal,
  isAsrResult,
  isConnected,
  isDeepseekTranslation,
  isError,
  isLlmTmtFinal,
  isTmtFinal,
  isTranslationResult,
  isWsMessage,
  WS_TYPE,
  type AsrFinalMessage,
  type AsrResultMessage,
  type ConnectedMessage,
  type LlmTmtFinalMessage,
  type PingMessage,
  type TmtFinalMessage,
  type TranslationResultMessage,
} from './protocol'

const asr: AsrResultMessage = {
  type: WS_TYPE.asr_result,
  callId: 'call-1',
  userId: 'user-1',
  text: 'We use a transformer architecture.',
  isFinal: true,
  confidence: 0.95,
  language: 'en',
  metadata: { utteranceId: 'call-1-utt-7' },
}

const asrFinal: AsrFinalMessage = {
  type: WS_TYPE.asr_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId: 'call-1-utt-7',
  text: 'We use a transformer architecture.',
  language: 'en',
}

const tmt: TranslationResultMessage = {
  type: WS_TYPE.translation_result,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId: 'call-1-utt-7',
  sourceText: 'We use a transformer architecture.',
  text: '我们使用变压器架构。',
  isFinal: false,
  engine: 'tmt',
  revised: false,
  language: 'zh',
}

const tmtFinal: TmtFinalMessage = {
  type: WS_TYPE.tmt_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId: 'call-1-utt-7',
  sourceText: 'We use a transformer architecture.',
  text: '我们使用变压器架构。',
  language: 'zh',
}

const llmTmtFinal: LlmTmtFinalMessage = {
  type: WS_TYPE.llm_tmt_final,
  callId: 'call-1',
  userId: 'user-1',
  utteranceId: 'call-1-utt-7',
  sourceText: 'We use a transformer architecture.',
  text: '我们采用一种 Transformer 架构。',
  revised: true,
  language: 'zh',
}

const deepseek: TranslationResultMessage = {
  ...tmt,
  text: '我们采用一种 Transformer 架构。',
  isFinal: true,
  engine: 'deepseek-flash',
  revised: true,
}

const connected: ConnectedMessage = {
  type: WS_TYPE.connected,
  connectionId: 'conn-1',
  tenantId: 'tenant-a',
  clientId: 'simulspeak-web',
}

const ping: PingMessage = { type: WS_TYPE.ping, requestId: 'p-1' }

describe('getUtteranceId', () => {
  it('reads asr_result utteranceId from metadata', () => {
    expect(getUtteranceId(asr)).toBe('call-1-utt-7')
  })

  it('reads translation_result utteranceId from top level', () => {
    expect(getUtteranceId(tmt)).toBe('call-1-utt-7')
  })

  it('reads the new final-flow utteranceId from top level', () => {
    expect(getUtteranceId(asrFinal)).toBe('call-1-utt-7')
    expect(getUtteranceId(tmtFinal)).toBe('call-1-utt-7')
    expect(getUtteranceId(llmTmtFinal)).toBe('call-1-utt-7')
  })

  it('asr and matching translation share the same utteranceId', () => {
    expect(getUtteranceId(asr)).toBe(getUtteranceId(deepseek))
    expect(getUtteranceId(asrFinal)).toBe(getUtteranceId(llmTmtFinal))
  })

  it('returns undefined when the message has no utteranceId', () => {
    expect(getUtteranceId(connected)).toBeUndefined()
    expect(getUtteranceId(ping)).toBeUndefined()
  })
})

describe('type guards', () => {
  it('narrows by type and stays mutually exclusive', () => {
    expect(isAsrResult(asr)).toBe(true)
    expect(isAsrResult(tmt)).toBe(false)
    expect(isTranslationResult(tmt)).toBe(true)
    expect(isTranslationResult(asr)).toBe(false)
    expect(isAsrFinal(asrFinal)).toBe(true)
    expect(isAsrFinal(asr)).toBe(false)
    expect(isTmtFinal(tmtFinal)).toBe(true)
    expect(isTmtFinal(tmt)).toBe(false)
    expect(isLlmTmtFinal(llmTmtFinal)).toBe(true)
    expect(isLlmTmtFinal(deepseek)).toBe(false)
    expect(isConnected(connected)).toBe(true)
    expect(isError(connected)).toBe(false)
  })
})

describe('isWsMessage', () => {
  it('accepts objects with a string type', () => {
    expect(isWsMessage(asr)).toBe(true)
  })

  it('rejects non-protocol values', () => {
    expect(isWsMessage(null)).toBe(false)
    expect(isWsMessage('asr_result')).toBe(false)
    expect(isWsMessage({})).toBe(false)
    expect(isWsMessage({ type: 1 })).toBe(false)
  })
})

describe('isDeepseekTranslation', () => {
  it('distinguishes deepseek-flash from tmt', () => {
    expect(isDeepseekTranslation(deepseek)).toBe(true)
    expect(isDeepseekTranslation(tmt)).toBe(false)
  })
})
