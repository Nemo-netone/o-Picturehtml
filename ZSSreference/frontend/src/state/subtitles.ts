import { create } from 'zustand'

import { MAX_SUBTITLE_LINES, THROTTLE_MS } from '../config'
import {
  getUtteranceId,
  isAiCorrectedEngine,
  isAsrFinal,
  isAsrResult,
  isDeepseekTranslation,
  isLlmTmtFinal,
  isTmtFinal,
  isTranslationResult,
  type AsrFinalMessage,
  type AsrResultMessage,
  type LlmTmtFinalMessage,
  type SubtitleLine,
  type TmtFinalMessage,
  type TranslationResultMessage,
  type WsMessage,
} from '../types/protocol'

export type SubtitleState = {
  lines: SubtitleLine[]
  byId: Map<string, SubtitleLine>
  nextSeq: number
  lastTmtPartialAtById: Map<string, number>
}

export type SubtitleStore = SubtitleState & {
  dispatch: (msg: WsMessage, now?: number) => void
  reset: () => void
}

export function createInitialSubtitleState(): SubtitleState {
  return {
    lines: [],
    byId: new Map(),
    nextSeq: 0,
    lastTmtPartialAtById: new Map(),
  }
}

function cloneState(state: SubtitleState): SubtitleState {
  return {
    lines: [...state.lines],
    byId: new Map(state.byId),
    nextSeq: state.nextSeq,
    lastTmtPartialAtById: new Map(state.lastTmtPartialAtById),
  }
}

function readUtteranceId(msg: WsMessage): string | undefined {
  try {
    const utteranceId = getUtteranceId(msg)
    return utteranceId && utteranceId.trim() ? utteranceId : undefined
  } catch {
    return undefined
  }
}

function createEmptyLine(utteranceId: string, seq: number): SubtitleLine {
  return {
    utteranceId,
    en: '',
    enFinal: false,
    zh: '',
    zhFinal: false,
    revised: false,
    seq,
  }
}

function upsertLine(draft: SubtitleState, utteranceId: string): SubtitleLine {
  const existing = draft.byId.get(utteranceId)

  if (existing) {
    return existing
  }

  const line = createEmptyLine(utteranceId, draft.nextSeq)
  draft.nextSeq += 1
  draft.byId.set(utteranceId, line)
  draft.lines.push(line)

  return line
}

function replaceLine(draft: SubtitleState, line: SubtitleLine) {
  draft.byId.set(line.utteranceId, line)
  draft.lines = draft.lines.map((item) =>
    item.utteranceId === line.utteranceId ? line : item,
  )
}

function trimSubtitleState(draft: SubtitleState): SubtitleState {
  if (draft.lines.length <= MAX_SUBTITLE_LINES) {
    return draft
  }

  const sorted = [...draft.lines].sort((a, b) => a.seq - b.seq)
  const retained = sorted.slice(-MAX_SUBTITLE_LINES)
  const retainedIds = new Set(retained.map((line) => line.utteranceId))

  for (const id of draft.byId.keys()) {
    if (!retainedIds.has(id)) {
      draft.byId.delete(id)
      draft.lastTmtPartialAtById.delete(id)
    }
  }

  draft.lines = retained
  return draft
}

export function applyAsrResult(
  state: SubtitleState,
  msg: AsrResultMessage,
): SubtitleState {
  const utteranceId = readUtteranceId(msg)

  if (!utteranceId) {
    return state
  }

  const existing = state.byId.get(utteranceId)
  if (existing?.enFinal && !msg.isFinal) {
    return state
  }

  const draft = cloneState(state)
  const current = upsertLine(draft, utteranceId)
  const next: SubtitleLine = {
    ...current,
    en: msg.text,
    enFinal: msg.isFinal || current.enFinal,
  }

  replaceLine(draft, next)
  return trimSubtitleState(draft)
}

export function applyAsrFinal(
  state: SubtitleState,
  msg: AsrFinalMessage,
): SubtitleState {
  const utteranceId = readUtteranceId(msg)

  if (!utteranceId) {
    return state
  }

  const draft = cloneState(state)
  const current = upsertLine(draft, utteranceId)
  const next: SubtitleLine = {
    ...current,
    en: msg.text,
    enFinal: true,
  }

  replaceLine(draft, next)
  return trimSubtitleState(draft)
}

export function applyTranslationResult(
  state: SubtitleState,
  msg: TranslationResultMessage,
  now = Date.now(),
): SubtitleState {
  const utteranceId = readUtteranceId(msg)

  if (!utteranceId) {
    return state
  }

  const existing = state.byId.get(utteranceId)
  const isDeepseek = isDeepseekTranslation(msg)

  if (existing?.zhFinal && !msg.isFinal) {
    return state
  }

  if (
    existing?.zhFinal &&
    isAiCorrectedEngine(existing.engine) &&
    !isDeepseek
  ) {
    return state
  }

  const isTmtPartial = msg.engine === 'tmt' && !msg.isFinal
  if (isTmtPartial) {
    const lastAppliedAt = state.lastTmtPartialAtById.get(utteranceId)
    if (lastAppliedAt !== undefined && now - lastAppliedAt < THROTTLE_MS) {
      return state
    }
  }

  const draft = cloneState(state)
  const current = upsertLine(draft, utteranceId)
  const next: SubtitleLine = {
    ...current,
    zh: msg.text,
    zhFinal: msg.isFinal || current.zhFinal,
    revised: msg.isFinal ? msg.revised : false,
    engine: msg.engine,
  }

  replaceLine(draft, next)

  if (isTmtPartial) {
    draft.lastTmtPartialAtById.set(utteranceId, now)
  }

  return trimSubtitleState(draft)
}

export function applyTmtFinal(
  state: SubtitleState,
  msg: TmtFinalMessage,
): SubtitleState {
  const utteranceId = readUtteranceId(msg)

  if (!utteranceId) {
    return state
  }

  const existing = state.byId.get(utteranceId)
  if (existing?.zhFinal && isAiCorrectedEngine(existing.engine)) {
    return state
  }

  const draft = cloneState(state)
  const current = upsertLine(draft, utteranceId)
  const next: SubtitleLine = {
    ...current,
    zh: msg.text,
    zhFinal: false,
    revised: false,
    engine: 'tmt',
  }

  replaceLine(draft, next)
  return trimSubtitleState(draft)
}

export function applyLlmTmtFinal(
  state: SubtitleState,
  msg: LlmTmtFinalMessage,
): SubtitleState {
  const utteranceId = readUtteranceId(msg)

  if (!utteranceId) {
    return state
  }

  const existing = state.byId.get(utteranceId)
  const revised =
    typeof msg.revised === 'boolean'
      ? msg.revised
      : (existing?.zh ?? '').trim() !== msg.text.trim()

  const draft = cloneState(state)
  const current = upsertLine(draft, utteranceId)
  const next: SubtitleLine = {
    ...current,
    zh: msg.text,
    zhFinal: true,
    revised,
    engine: 'llm-tmt',
  }

  replaceLine(draft, next)
  return trimSubtitleState(draft)
}

export function reduceSubtitleMessage(
  state: SubtitleState,
  msg: WsMessage,
  now?: number,
): SubtitleState {
  if (isAsrFinal(msg)) {
    return applyAsrFinal(state, msg)
  }

  if (isTmtFinal(msg)) {
    return applyTmtFinal(state, msg)
  }

  if (isLlmTmtFinal(msg)) {
    return applyLlmTmtFinal(state, msg)
  }

  if (isAsrResult(msg)) {
    return applyAsrResult(state, msg)
  }

  if (isTranslationResult(msg)) {
    return applyTranslationResult(state, msg, now)
  }

  return state
}

export const useSubtitleStore = create<SubtitleStore>((set) => ({
  ...createInitialSubtitleState(),
  dispatch: (msg: WsMessage, now?: number) => {
    set((state) => reduceSubtitleMessage(state, msg, now))
  },
  reset: () => {
    set(createInitialSubtitleState())
  },
}))
