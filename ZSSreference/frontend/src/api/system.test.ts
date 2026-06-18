import { describe, expect, it } from 'vitest'

import { SystemApiError, createSystemApiClient } from './system'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

describe('createSystemApiClient', () => {
  it('fetches system nodes from the api v1 endpoint', async () => {
    const calls: string[] = []
    const client = createSystemApiClient({
      baseUrl: 'http://api.local/',
      fetcher: async (input) => {
        calls.push(String(input))
        return jsonResponse({
          code: 200,
          message: 'ok',
          data: {
            refreshedAt: '2026-06-07T00:00:00Z',
            media: { summary: {}, nodes: [] },
            workers: { summary: {}, nodes: [] },
          },
        })
      },
    })

    await expect(client.getNodes()).resolves.toMatchObject({
      media: { nodes: [] },
      workers: { nodes: [] },
    })
    expect(calls[0]).toBe('http://api.local/api/v1/system/nodes')
  })

  it('throws a structured error for backend failures', async () => {
    const client = createSystemApiClient({
      baseUrl: '',
      fetcher: async () =>
        jsonResponse(
          {
            code: 503,
            message: 'service unavailable',
            error: 'registry unavailable',
          },
          503,
        ),
    })

    await expect(client.getNodes()).rejects.toMatchObject({
      name: 'SystemApiError',
      status: 503,
      code: 503,
      message: 'registry unavailable',
    } satisfies Partial<SystemApiError>)
  })
})
