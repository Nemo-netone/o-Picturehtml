/**
 * Mock WS 服务端（联调基座，conventions §5）。
 *
 * 不依赖真实后端，按 fixtures/*.json 的标准回放序列把 asr/translation 帧按
 * 时序回推给前端，用于 M1–M3 独立推进。帧格式逐字遵循 docs/05-interfaces.md A 节。
 *
 * 行为：
 *  - 接受 ws://host:PORT/ws?token=tenantId:clientId，连接即发 connected。
 *  - 收到 client_hello → 回 client_hello_ack，并开始回放默认 fixture。
 *  - 收到 ping → 回 pong；set_strategy → 回 set_strategy_ack。
 *  - 收到 webrtc_offer → 回一个占位 webrtc_answer（PR-7 不接真实媒体）。
 *
 * 运行：node mock/server.mjs [port]   （默认 8080，路径 /ws）
 */

import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { WebSocketServer } from 'ws'

const __dirname = dirname(fileURLToPath(import.meta.url))
const FIXTURES_DIR = join(__dirname, 'fixtures')
const PORT = Number(process.argv[2] ?? 8080)
const DEFAULT_FIXTURE = 'one-utterance.json'

function loadFixture(name) {
  const raw = readFileSync(join(FIXTURES_DIR, name), 'utf8')
  return JSON.parse(raw)
}

function send(ws, message) {
  ws.send(JSON.stringify(message))
}

/** 按 delayMs 时序回放一个 fixture 的所有帧。 */
function replay(ws, fixture) {
  let elapsed = 0
  for (const { delayMs, frame } of fixture.frames) {
    elapsed += delayMs ?? 0
    setTimeout(() => {
      if (ws.readyState === ws.OPEN) {
        send(ws, frame)
      }
    }, elapsed)
  }
  console.log(
    `[mock] replaying ${fixture.frames.length} frames over ~${elapsed}ms`,
  )
}

const wss = new WebSocketServer({ port: PORT, path: '/ws' })
let connSeq = 0

wss.on('connection', (ws, req) => {
  const url = new URL(req.url, `http://localhost:${PORT}`)
  const token = url.searchParams.get('token') ?? ''
  const [tenantId = 'tenant-a', clientId = 'simulspeak-web'] = token.split(':')
  connSeq += 1
  const connectionId = `conn-${connSeq}`
  console.log(`[mock] connection ${connectionId} token=${tenantId}:${clientId}`)

  send(ws, { type: 'connected', connectionId, tenantId, clientId })

  ws.on('message', (data) => {
    let msg
    try {
      msg = JSON.parse(data.toString())
    } catch {
      console.warn('[mock] ignoring invalid JSON frame')
      return
    }

    switch (msg.type) {
      case 'client_hello':
        send(ws, {
          type: 'client_hello_ack',
          requestId: msg.requestId,
          connectionId,
          responseMode: msg.responseMode ?? 'compact',
        })
        replay(ws, loadFixture(DEFAULT_FIXTURE))
        break
      case 'ping':
        send(ws, { type: 'pong', requestId: msg.requestId, connectionId })
        break
      case 'set_strategy':
        send(ws, {
          type: 'set_strategy_ack',
          requestId: msg.requestId,
          metadata: msg.metadata,
        })
        break
      case 'webrtc_offer':
        send(ws, {
          type: 'webrtc_answer',
          requestId: msg.requestId,
          callId: msg.callId,
          userId: msg.userId,
          sdp: 'v=0\r\n(mock answer)\r\n',
        })
        break
      default:
        // ice / tts_command 等暂忽略，PR-7 不接真实媒体。
        break
    }
  })

  ws.on('close', () => console.log(`[mock] ${connectionId} closed`))
})

console.log(
  `[mock] WS server listening on ws://localhost:${PORT}/ws`,
  `(fixtures: ${readdirSync(FIXTURES_DIR).join(', ')})`,
)
