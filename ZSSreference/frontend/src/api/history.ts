export type ApiEnvelope<T> = {
  code: number
  message?: string
  data?: T
  error?: string
}

export type InterpreterSessionSummary = {
  id: string
  tenantId: string
  connectionId?: string
  userId?: string
  caller?: string
  callee?: string
  state: string
  mediaState?: string
  providerIds?: Record<string, number[]>
  translateStrategy?: string
  dubbingEnabled: boolean
  startedAt?: string
  endedAt?: string
  createdAt?: string
  updatedAt?: string
  metadata?: Record<string, unknown>
}

export type InterpreterSessionListResult = {
  items: InterpreterSessionSummary[]
  total: number
  limit: number
  offset: number
}

export type InterpreterMTRecord = {
  id: number
  asrCallbackId: number
  sourceText?: string
  targetText?: string
  isFinal: boolean
  status?: string
  errorMessage?: string
  latencyMs?: number
}

export type InterpreterLLMRecord = {
  id: number
  asrCallbackId: number
  sourceText?: string
  draftTranslation?: string
  revisedText?: string
  revised: boolean
  status?: string
  errorMessage?: string
  latencyMs?: number
}

export type InterpreterASRCallback = {
  id: number
  sessionId: string
  callId: string
  utteranceId: string
  sequenceNo: number
  language?: string
  text: string
  isFinal: boolean
  confidence?: number
  startMs?: number
  endMs?: number
  receivedAt?: string
  mtTranslations?: InterpreterMTRecord[]
  llmRevisions?: InterpreterLLMRecord[]
}

export type InterpreterUtteranceDetail = {
  utteranceId: string
  asrCallbacks: InterpreterASRCallback[]
}

export type InterpreterSessionDetailResult = {
  session: InterpreterSessionSummary
  utterances: InterpreterUtteranceDetail[]
}

export type VocabularyTaskStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | string

export type VocabularyTaskResult = {
  id: string
  sessionId: string
  tenantId: string
  userId?: string
  partitionKey: string
  status: VocabularyTaskStatus
  maxWords: number
  englishSource?: string
  attemptCount: number
  lockedBy?: string
  lockedAt?: string
  startedAt?: string
  finishedAt?: string
  errorMessage?: string
  input?: unknown
  createdAt?: string
  updatedAt?: string
  metadata?: Record<string, unknown>
}

export type VocabularyTaskListResult = {
  items: VocabularyTaskResult[]
  total: number
  limit: number
  offset: number
}

export type VocabularyEntryResult = {
  id?: number
  taskId?: string
  ordinal: number
  word: string
  lemma?: string
  phonetic?: string
  partOfSpeech?: string
  meaningZh?: string
  exampleEn?: string
  exampleZh?: string
  occurrences?: number
  difficulty?: string
  sourceUtteranceIds?: unknown
  metadata?: Record<string, unknown>
}

export type VocabularyTaskDetailResult = {
  task: VocabularyTaskResult
  entries?: VocabularyEntryResult[]
}

export type SessionListQuery = {
  tenantId?: string
  state?: string
  limit?: number
  offset?: number
}

export type VocabularyTaskListQuery = {
  status?: string
  limit?: number
  offset?: number
}

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

export class HistoryApiError extends Error {
  readonly status: number
  readonly code?: number

  constructor(message: string, status: number, code?: number) {
    super(message)
    this.name = 'HistoryApiError'
    this.status = status
    this.code = code
  }
}

export type HistoryApiClientOptions = {
  baseUrl: string
  fetcher?: FetchLike
}

export function buildApiUrl(
  baseUrl: string,
  path: string,
  query: Record<string, string | number | undefined> = {},
) {
  const normalizedPath = `/api/v1${path}`
  const trimmedBaseUrl = baseUrl.trim()

  if (!trimmedBaseUrl || trimmedBaseUrl === '/') {
    const params = new URLSearchParams()

    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== '') {
        params.set(key, String(value))
      }
    }

    const queryString = params.toString()
    return queryString ? `${normalizedPath}?${queryString}` : normalizedPath
  }

  const base = trimmedBaseUrl.replace(/\/$/, '')
  const url = new URL(normalizedPath, `${base}/`)

  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') {
      url.searchParams.set(key, String(value))
    }
  }

  return url.toString()
}

async function readEnvelope<T>(response: Response): Promise<T> {
  let envelope: ApiEnvelope<T> | undefined

  try {
    envelope = (await response.json()) as ApiEnvelope<T>
  } catch {
    if (!response.ok) {
      throw new HistoryApiError(
        response.statusText || 'HTTP 请求失败',
        response.status,
      )
    }
  }

  const code = envelope?.code
  const message = envelope?.error || envelope?.message || response.statusText

  if (!response.ok || (typeof code === 'number' && code >= 400)) {
    throw new HistoryApiError(message || 'HTTP 请求失败', response.status, code)
  }

  if (!envelope || envelope.data === undefined) {
    throw new HistoryApiError('响应缺少 data 字段', response.status, code)
  }

  return envelope.data
}

export function createHistoryApiClient({
  baseUrl,
  fetcher = fetch,
}: HistoryApiClientOptions) {
  const request = async <T>(path: string, init?: RequestInit) => {
    const response = await fetcher(buildApiUrl(baseUrl, path), init)
    return readEnvelope<T>(response)
  }

  return {
    listSessions(query: SessionListQuery = {}) {
      return fetcher(
        buildApiUrl(baseUrl, '/interpreter/sessions', {
          tenantId: query.tenantId,
          state: query.state,
          limit: query.limit,
          offset: query.offset,
        }),
      ).then(readEnvelope<InterpreterSessionListResult>)
    },

    getSessionDetail(callID: string) {
      return request<InterpreterSessionDetailResult>(
        `/interpreter/sessions/${encodeURIComponent(callID)}`,
      )
    },

    createVocabularyTask(callID: string, maxWords: number) {
      return request<VocabularyTaskResult>(
        `/interpreter/sessions/${encodeURIComponent(callID)}/vocabulary-tasks`,
        {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ maxWords }),
        },
      )
    },

    listVocabularyTasks(callID: string, query: VocabularyTaskListQuery = {}) {
      return fetcher(
        buildApiUrl(
          baseUrl,
          `/interpreter/sessions/${encodeURIComponent(callID)}/vocabulary-tasks`,
          {
            status: query.status,
            limit: query.limit,
            offset: query.offset,
          },
        ),
      ).then(readEnvelope<VocabularyTaskListResult>)
    },

    getVocabularyTask(taskID: string) {
      return request<VocabularyTaskDetailResult>(
        `/vocabulary-tasks/${encodeURIComponent(taskID)}`,
      )
    },
  }
}
