import { describe, expect, it } from 'vitest'

import type { InterpreterSessionSummary } from '../api/history'
import {
  HISTORY_PAGE_LIMIT,
  buildSessionListQuery,
  canGoNextPage,
  getDefaultSelectedSessionId,
  isVocabularyTaskTerminal,
  nextOffset,
  previousOffset,
  validateMaxWords,
} from './historyModel'

function session(id: string): InterpreterSessionSummary {
  return {
    id,
    tenantId: 'tenant-a',
    state: 'ended',
    dubbingEnabled: true,
  }
}

describe('historyModel', () => {
  it('selects the first returned session unless the current one still exists', () => {
    const sessions = [session('call-new'), session('call-old')]

    expect(getDefaultSelectedSessionId(sessions)).toBe('call-new')
    expect(getDefaultSelectedSessionId(sessions, 'call-old')).toBe('call-old')
    expect(getDefaultSelectedSessionId(sessions, 'missing')).toBe('call-new')
    expect(getDefaultSelectedSessionId([])).toBeUndefined()
  })

  it('builds list queries with defaults and trimmed optional filters', () => {
    expect(
      buildSessionListQuery({
        tenantId: ' tenant-a ',
        state: 'ended',
        offset: 20,
      }),
    ).toEqual({
      tenantId: 'tenant-a',
      state: 'ended',
      limit: HISTORY_PAGE_LIMIT,
      offset: 20,
    })

    expect(
      buildSessionListQuery({ tenantId: ' ', state: '', offset: 0 }),
    ).toEqual({ limit: HISTORY_PAGE_LIMIT, offset: 0 })
  })

  it('validates maxWords before creating vocabulary tasks', () => {
    expect(validateMaxWords('30')).toEqual({ value: 30 })
    expect(validateMaxWords('0')).toEqual({
      error: '单词数量范围为 1-100。',
    })
    expect(validateMaxWords('101')).toEqual({
      error: '单词数量范围为 1-100。',
    })
    expect(validateMaxWords('3.5')).toEqual({
      error: '请输入 1-100 的整数。',
    })
  })

  it('detects task terminal states and pagination boundaries', () => {
    expect(isVocabularyTaskTerminal('pending')).toBe(false)
    expect(isVocabularyTaskTerminal('running')).toBe(false)
    expect(isVocabularyTaskTerminal('succeeded')).toBe(true)
    expect(isVocabularyTaskTerminal('failed')).toBe(true)
    expect(isVocabularyTaskTerminal('cancelled')).toBe(true)
    expect(canGoNextPage(41, 20, 20)).toBe(true)
    expect(canGoNextPage(40, 20, 20)).toBe(false)
    expect(previousOffset(10, 20)).toBe(0)
    expect(nextOffset(20, 20)).toBe(40)
  })
})
