import { buildApiUrl, type ApiEnvelope } from './history'

export type NodeStatus = 'up' | 'down' | 'draining' | 'suspect' | string

export type SystemNode = {
  id: string
  type: string
  endpoint: string
  zone?: string
  status: NodeStatus
  weight?: number
  maxCalls: number
  currentCalls: number
  nodeVersion?: string
  startedAt?: string
  leaseId?: number
  capabilities?: string[]
  labels?: Record<string, string>
}

export type MediaNodeSummary = {
  total: number
  available: number
  unavailable: number
  up: number
  down: number
  draining: number
  suspect: number
  capacity: number
  currentCalls: number
}

export type WorkerNodeSummary = {
  total: number
  available: number
  unavailable: number
  up: number
  down: number
  draining: number
  suspect: number
  capacity: number
  activeTasks: number
}

export type MediaNodesResult = {
  summary: MediaNodeSummary
  nodes: SystemNode[]
}

export type WorkerNodesResult = {
  summary: WorkerNodeSummary
  nodes: SystemNode[]
}

export type SystemNodesResult = {
  refreshedAt: string
  media: MediaNodesResult
  workers: WorkerNodesResult
}

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

export class SystemApiError extends Error {
  readonly status: number
  readonly code?: number

  constructor(message: string, status: number, code?: number) {
    super(message)
    this.name = 'SystemApiError'
    this.status = status
    this.code = code
  }
}

export type SystemApiClientOptions = {
  baseUrl: string
  fetcher?: FetchLike
}

async function readEnvelope<T>(response: Response): Promise<T> {
  let envelope: ApiEnvelope<T> | undefined

  try {
    envelope = (await response.json()) as ApiEnvelope<T>
  } catch {
    if (!response.ok) {
      throw new SystemApiError(
        response.statusText || 'HTTP 请求失败',
        response.status,
      )
    }
  }

  const code = envelope?.code
  const message = envelope?.error || envelope?.message || response.statusText

  if (!response.ok || (typeof code === 'number' && code >= 400)) {
    throw new SystemApiError(message || 'HTTP 请求失败', response.status, code)
  }

  if (!envelope || envelope.data === undefined) {
    throw new SystemApiError('响应缺少 data 字段', response.status, code)
  }

  return envelope.data
}

export function createSystemApiClient({
  baseUrl,
  fetcher = fetch,
}: SystemApiClientOptions) {
  return {
    getNodes() {
      return fetcher(buildApiUrl(baseUrl, '/system/nodes')).then(
        readEnvelope<SystemNodesResult>,
      )
    },
  }
}
