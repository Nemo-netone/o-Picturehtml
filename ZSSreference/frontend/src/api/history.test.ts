import { describe, expect, it } from 'vitest'

import {
  HistoryApiError,
  buildApiUrl,
  createHistoryApiClient,
} from './history'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

describe('buildApiUrl', () => {
  it('builds same-origin relative URLs when baseUrl is empty', () => {
    expect(
      buildApiUrl('', '/interpreter/sessions', {
        tenantId: 'tenant a',
        limit: 20,
        offset: 0,
      }),
    ).toBe('/api/v1/interpreter/sessions?tenantId=tenant+a&limit=20&offset=0')
  })

  it('joins the api v1 prefix and skips empty query values', () => {
    expect(
      buildApiUrl('http://127.0.0.1:8080/', '/interpreter/sessions', {
        tenantId: 'tenant-a',
        state: '',
        limit: 20,
        offset: 0,
      }),
    ).toBe(
      'http://127.0.0.1:8080/api/v1/interpreter/sessions?tenantId=tenant-a&limit=20&offset=0',
    )
  })
})

describe('createHistoryApiClient', () => {
  it('lists sessions with encoded query parameters', async () => {
    const calls: string[] = []
    const client = createHistoryApiClient({
      baseUrl: 'http://api.local',
      fetcher: async (input) => {
        calls.push(String(input))
        return jsonResponse({
          code: 200,
          message: 'ok',
          data: { items: [], total: 0, limit: 20, offset: 0 },
        })
      },
    })

    await expect(
      client.listSessions({
        tenantId: 'tenant a',
        state: 'ended',
        limit: 20,
        offset: 40,
      }),
    ).resolves.toMatchObject({ total: 0 })

    expect(calls[0]).toBe(
      'http://api.local/api/v1/interpreter/sessions?tenantId=tenant+a&state=ended&limit=20&offset=40',
    )
  })

  it('creates vocabulary tasks with a JSON body', async () => {
    const calls: { input: string; init?: RequestInit }[] = []
    const client = createHistoryApiClient({
      baseUrl: 'http://api.local',
      fetcher: async (input, init) => {
        calls.push({ input: String(input), init })
        return jsonResponse({
          code: 202,
          message: 'ok',
          data: {
            id: 'task-1',
            sessionId: 'call 1',
            tenantId: 'tenant-a',
            partitionKey: 'tenant-a:user-a',
            status: 'pending',
            maxWords: 30,
            attemptCount: 0,
          },
        })
      },
    })

    await client.createVocabularyTask('call 1', 30)

    expect(calls[0].input).toBe(
      'http://api.local/api/v1/interpreter/sessions/call%201/vocabulary-tasks',
    )
    expect(calls[0].init?.method).toBe('POST')
    expect(calls[0].init?.body).toBe(JSON.stringify({ maxWords: 30 }))
  })

  it('throws a structured error for backend failures', async () => {
    const client = createHistoryApiClient({
      baseUrl: 'http://api.local',
      fetcher: async () =>
        jsonResponse(
          {
            code: 404,
            message: 'not found',
            error: 'missing session',
          },
          404,
        ),
    })

    await expect(client.getSessionDetail('missing')).rejects.toMatchObject({
      name: 'HistoryApiError',
      status: 404,
      code: 404,
      message: 'missing session',
    } satisfies Partial<HistoryApiError>)
  })
})
