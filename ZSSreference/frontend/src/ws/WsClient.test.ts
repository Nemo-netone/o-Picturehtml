import { describe, expect, it, vi } from 'vitest'
import { WS_TYPE, type WsMessage } from '../types/protocol'
import {
  buildWsUrl,
  WsClient,
  type WebSocketLike,
  type WsClientOptions,
} from './WsClient'

/** 可手动驱动 open/message/close 的假 socket。 */
class FakeSocket implements WebSocketLike {
  readyState = 0
  sent: string[] = []
  url: string
  onopen: ((event: unknown) => void) | null = null
  onclose: ((event: unknown) => void) | null = null
  onerror: ((event: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null

  constructor(url: string) {
    this.url = url
  }

  send(data: string): void {
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 3
    this.onclose?.({})
  }

  emitOpen(): void {
    this.readyState = 1
    this.onopen?.({})
  }

  emit(data: unknown): void {
    this.onmessage?.({ data })
  }
}

function makeClient(extra?: Partial<WsClientOptions>) {
  let socket: FakeSocket | undefined
  const client = new WsClient({
    url: 'ws://localhost:8080/ws',
    tenantId: 'tenant-a',
    clientId: 'simulspeak-web',
    handshake: { translateStrategy: 'hybrid', dubbing: '1' },
    socketFactory: (url) => {
      socket = new FakeSocket(url)
      return socket
    },
    ...extra,
  })
  return { client, getSocket: () => socket! }
}

describe('buildWsUrl', () => {
  it('assembles the token query from structured fields', () => {
    expect(buildWsUrl('ws://h/ws', 'tenant-a', 'web')).toBe(
      'ws://h/ws?token=tenant-a%3Aweb',
    )
  })
})

describe('WsClient connection', () => {
  it('transitions idle → connecting → open → closed', () => {
    const { client, getSocket } = makeClient()
    const seen: string[] = []
    client.onStatusChange((s) => seen.push(s))

    expect(client.getStatus()).toBe('idle')
    client.connect()
    expect(client.getStatus()).toBe('connecting')
    getSocket().emitOpen()
    expect(client.getStatus()).toBe('open')
    expect(client.isOpen()).toBe(true)
    client.close()
    expect(client.getStatus()).toBe('closed')
    expect(seen).toEqual(['connecting', 'open', 'closed'])
  })

  it('connect is idempotent', () => {
    const { client, getSocket } = makeClient()
    client.connect()
    const first = getSocket()
    client.connect()
    expect(getSocket()).toBe(first)
  })
})

describe('WsClient requestId', () => {
  it('increments and honors a prefix', () => {
    const { client } = makeClient()
    expect(client.nextRequestId('hello')).toBe('hello-1')
    expect(client.nextRequestId('hello')).toBe('hello-2')
    expect(client.nextRequestId()).toBe('req-3')
  })
})

describe('WsClient codec', () => {
  it('JSON-encodes outbound messages when open', () => {
    const { client, getSocket } = makeClient()
    client.connect()
    getSocket().emitOpen()
    client.send({ type: WS_TYPE.ping, requestId: 'p-1' })
    expect(JSON.parse(getSocket().sent[0])).toEqual({
      type: 'ping',
      requestId: 'p-1',
    })
  })

  it('drops sends when the socket is not open', () => {
    const { client, getSocket } = makeClient()
    client.connect()
    client.send({ type: WS_TYPE.ping, requestId: 'p-1' })
    expect(getSocket().sent).toHaveLength(0)
  })

  it('decodes inbound frames and emits to listeners', () => {
    const { client, getSocket } = makeClient()
    const received: WsMessage[] = []
    client.onMessage((m) => received.push(m))
    client.connect()
    getSocket().emitOpen()
    getSocket().emit(
      JSON.stringify({
        type: 'asr_result',
        callId: 'call-1',
        userId: 'user-1',
        text: 'hi',
        isFinal: false,
        metadata: { utteranceId: 'call-1-utt-1' },
      }),
    )
    expect(received).toHaveLength(1)
    expect(received[0].type).toBe('asr_result')
  })

  it('drops invalid JSON and non-protocol frames without throwing', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { client, getSocket } = makeClient()
    const received: WsMessage[] = []
    client.onMessage((m) => received.push(m))
    client.connect()
    getSocket().emitOpen()
    getSocket().emit('not json {')
    getSocket().emit(JSON.stringify({ noType: true }))
    getSocket().emit(42)
    expect(received).toHaveLength(0)
    warn.mockRestore()
  })

  it('stops delivering after unsubscribe', () => {
    const { client, getSocket } = makeClient()
    const received: WsMessage[] = []
    const off = client.onMessage((m) => received.push(m))
    client.connect()
    getSocket().emitOpen()
    off()
    getSocket().emit(
      JSON.stringify({ type: 'pong', requestId: 'p-1', connectionId: 'c-1' }),
    )
    expect(received).toHaveLength(0)
  })
})

describe('WsClient heartbeat', () => {
  it('sends a ping every interval once open', () => {
    vi.useFakeTimers()
    try {
      const { client, getSocket } = makeClient({
        heartbeatIntervalMs: 1000,
        heartbeatTimeoutMs: 500,
      })
      client.connect()
      getSocket().emitOpen()
      expect(getSocket().sent).toHaveLength(0)
      vi.advanceTimersByTime(1000)
      const first = JSON.parse(getSocket().sent[0])
      expect(first.type).toBe('ping')
      expect(first.requestId).toMatch(/^ping-/)
    } finally {
      vi.useRealTimers()
    }
  })

  it('clears the timeout when a pong arrives in time', () => {
    vi.useFakeTimers()
    try {
      const { client, getSocket } = makeClient({
        heartbeatIntervalMs: 1000,
        heartbeatTimeoutMs: 500,
      })
      client.connect()
      getSocket().emitOpen()
      vi.advanceTimersByTime(1000)
      getSocket().emit(
        JSON.stringify({
          type: 'pong',
          requestId: 'ping-1',
          connectionId: 'c-1',
        }),
      )
      vi.advanceTimersByTime(1000)
      expect(client.getStatus()).toBe('open')
    } finally {
      vi.useRealTimers()
    }
  })

  it('closes the connection when pong times out', () => {
    vi.useFakeTimers()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    try {
      const { client, getSocket } = makeClient({
        heartbeatIntervalMs: 1000,
        heartbeatTimeoutMs: 500,
      })
      client.connect()
      getSocket().emitOpen()
      vi.advanceTimersByTime(1000) // ping sent, pong timer armed
      vi.advanceTimersByTime(500) // no pong → timeout
      expect(client.getStatus()).toBe('closed')
    } finally {
      warn.mockRestore()
      vi.useRealTimers()
    }
  })

  it('stops pinging after close', () => {
    vi.useFakeTimers()
    try {
      const { client, getSocket } = makeClient({ heartbeatIntervalMs: 1000 })
      client.connect()
      const socket = getSocket()
      socket.emitOpen()
      client.close()
      vi.advanceTimersByTime(5000)
      expect(socket.sent).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('WsClient handshake', () => {
  it('sends client_hello on connected with compact mode and metadata', () => {
    const { client, getSocket } = makeClient({
      handshake: {
        translateStrategy: 'tmt',
        dubbing: '0',
        sourceLanguage: 'fr',
        targetLanguage: 'zh',
        asrEngineType: '16k_fr',
        ttsVoiceType: '101001',
      },
    })
    client.connect()
    getSocket().emitOpen()
    getSocket().emit(
      JSON.stringify({
        type: 'connected',
        connectionId: 'conn-1',
        tenantId: 'tenant-a',
        clientId: 'simulspeak-web',
      }),
    )
    const hello = JSON.parse(getSocket().sent[0])
    expect(hello.type).toBe('client_hello')
    expect(hello.responseMode).toBe('compact')
    expect(hello.requestId).toMatch(/^hello-/)
    expect(hello.metadata).toEqual({
      translateStrategy: 'tmt',
      dubbing: '0',
      sourceLanguage: 'fr',
      targetLanguage: 'zh',
      asrEngineType: '16k_fr',
      ttsVoiceType: '101001',
    })
  })
})

describe('WsClient typed dispatch', () => {
  it('routes a frame only to listeners of its type', () => {
    const { client, getSocket } = makeClient()
    const asr: WsMessage[] = []
    const tr: WsMessage[] = []
    client.on('asr_result', (m) => asr.push(m))
    client.on('translation_result', (m) => tr.push(m))
    client.connect()
    getSocket().emitOpen()
    getSocket().emit(
      JSON.stringify({
        type: 'asr_result',
        callId: 'call-1',
        userId: 'user-1',
        text: 'hi',
        isFinal: false,
        metadata: { utteranceId: 'u-1' },
      }),
    )
    expect(asr).toHaveLength(1)
    expect(tr).toHaveLength(0)
  })

  it('routes new final-flow frames to typed listeners', () => {
    const { client, getSocket } = makeClient()
    const asrFinal: WsMessage[] = []
    const tmtFinal: WsMessage[] = []
    const llmFinal: WsMessage[] = []
    const legacyAsr: WsMessage[] = []
    client.on('asr_final', (m) => asrFinal.push(m))
    client.on('tmt_final', (m) => tmtFinal.push(m))
    client.on('llm_tmt_final', (m) => llmFinal.push(m))
    client.on('asr_result', (m) => legacyAsr.push(m))
    client.connect()
    getSocket().emitOpen()

    getSocket().emit(
      JSON.stringify({
        type: 'asr_final',
        callId: 'call-1',
        userId: 'user-1',
        utteranceId: 'u-1',
        text: 'Final sentence.',
        language: 'en',
      }),
    )
    getSocket().emit(
      JSON.stringify({
        type: 'tmt_final',
        callId: 'call-1',
        userId: 'user-1',
        utteranceId: 'u-1',
        text: '机器翻译',
        language: 'zh',
      }),
    )
    getSocket().emit(
      JSON.stringify({
        type: 'llm_tmt_final',
        callId: 'call-1',
        userId: 'user-1',
        utteranceId: 'u-1',
        text: 'AI 矫正翻译',
        revised: true,
        language: 'zh',
      }),
    )

    expect(asrFinal).toHaveLength(1)
    expect(tmtFinal).toHaveLength(1)
    expect(llmFinal).toHaveLength(1)
    expect(legacyAsr).toHaveLength(0)
  })

  it('stops routing after the typed unsubscribe', () => {
    const { client, getSocket } = makeClient()
    const seen: WsMessage[] = []
    const off = client.on('error', (m) => seen.push(m))
    client.connect()
    getSocket().emitOpen()
    off()
    getSocket().emit(
      JSON.stringify({ type: 'error', requestId: 'x', error: 'boom' }),
    )
    expect(seen).toHaveLength(0)
  })
})

describe('WsClient error frame', () => {
  it('logs at error level and still dispatches to subscribers', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const { client, getSocket } = makeClient()
      const seen: WsMessage[] = []
      client.on('error', (m) => seen.push(m))
      client.connect()
      getSocket().emitOpen()
      getSocket().emit(
        JSON.stringify({
          type: 'error',
          requestId: 'x',
          callId: 'call-1',
          error: 'provider missing',
        }),
      )
      expect(seen).toHaveLength(1)
      expect(err).toHaveBeenCalled()
    } finally {
      err.mockRestore()
    }
  })
})
