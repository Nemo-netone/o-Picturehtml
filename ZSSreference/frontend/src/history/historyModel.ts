import type {
  InterpreterSessionSummary,
  SessionListQuery,
  VocabularyTaskStatus,
} from '../api/history'

export const HISTORY_PAGE_LIMIT = 20
export const TASK_LIST_LIMIT = 20
export const DEFAULT_MAX_WORDS = 30
export const MIN_MAX_WORDS = 1
export const MAX_MAX_WORDS = 100
export const TASK_POLL_INTERVAL_MS = 2500

export type SessionStateFilter = '' | 'active' | 'ended' | 'failed'

export const SESSION_STATE_OPTIONS: {
  value: SessionStateFilter
  label: string
}[] = [
  { value: '', label: '全部状态' },
  { value: 'active', label: '进行中' },
  { value: 'ended', label: '已结束' },
  { value: 'failed', label: '失败' },
]

export function isVocabularyTaskTerminal(status: VocabularyTaskStatus) {
  return status === 'succeeded' || status === 'failed' || status === 'cancelled'
}

export function getDefaultSelectedSessionId(
  sessions: InterpreterSessionSummary[],
  currentId?: string,
) {
  if (currentId && sessions.some((session) => session.id === currentId)) {
    return currentId
  }

  return sessions[0]?.id
}

export function buildSessionListQuery({
  tenantId,
  state,
  offset,
}: {
  tenantId: string
  state: SessionStateFilter
  offset: number
}): SessionListQuery {
  return {
    tenantId: tenantId.trim() || undefined,
    state: state || undefined,
    limit: HISTORY_PAGE_LIMIT,
    offset,
  }
}

export function validateMaxWords(rawValue: string) {
  const trimmed = rawValue.trim()
  const value = Number(trimmed)

  if (!trimmed || !Number.isInteger(value)) {
    return { error: '请输入 1-100 的整数。' }
  }

  if (value < MIN_MAX_WORDS || value > MAX_MAX_WORDS) {
    return { error: '单词数量范围为 1-100。' }
  }

  return { value }
}

export function canGoNextPage(total: number, offset: number, limit: number) {
  return offset + limit < total
}

export function previousOffset(offset: number, limit: number) {
  return Math.max(0, offset - limit)
}

export function nextOffset(offset: number, limit: number) {
  return offset + limit
}
