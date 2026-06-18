/**
 * WS 控制客户端（基础层）。
 *
 * 职责（PR-4 范围）：建立连接、JSON 自动编解码、requestId 生成、连接状态与
 * 原始消息的订阅分发。心跳(ping/pong)、握手(client_hello)、按 type 的路由与
 * error 帧语义分别在后续 PR 叠加，不在本层耦合。
 *
 * 分层约束(conventions §1.2)：本文件不得 import components/ 或 state/，只产出
 * 事件/回调，由 session/ 编排写入 store。
 */

import { HEARTBEAT_INTERVAL_MS, HEARTBEAT_TIMEOUT_MS } from '../config'
import {
  isConnected,
  isError,
  isPong,
  isWsMessage,
  WS_TYPE,
  type AsrEngineType,
  type ClientHelloMessage,
  type DubbingFlag,
  type OutboundMessage,
  type ResponseMode,
  type SourceLanguage,
  type Strategy,
  type TargetLanguage,
  type TtsVoiceType,
  type WsMessage,
  type WsType,
} from '../types/protocol'

/** WebSocket.readyState：OPEN 常量（不依赖 node 环境的全局 WebSocket）。 */
const WS_OPEN = 1

/** 连接状态机。 */
export type WsConnectionStatus = 'idle' | 'connecting' | 'open' | 'closed'

/** 仅依赖浏览器 WebSocket 的最小子集，便于注入 mock 做单测。 */
export interface WebSocketLike {
  readyState: number
  send(data: string): void
  close(code?: number, reason?: string): void
  onopen: ((event: unknown) => void) | null
  onclose: ((event: unknown) => void) | null
  onerror: ((event: unknown) => void) | null
  onmessage: ((event: { data: unknown }) => void) | null
}

/** 由结构化字段建连，token 在内部组装，调用方不手拼字符串(conventions §6)。 */
export type WsClientOptions = {
  /** 基础地址，如 ws://localhost:8080/ws（来自 config.appConfig.wsUrl）。 */
  url: string
  tenantId: string
  clientId: string
  /** 可注入的 socket 工厂，默认使用全局 WebSocket；单测注入 mock。 */
  socketFactory?: (url: string) => WebSocketLike
  /** ping 发送间隔（ms），默认 config.HEARTBEAT_INTERVAL_MS；测试可调小。 */
  heartbeatIntervalMs?: number
  /** pong 超时阈值（ms），默认 config.HEARTBEAT_TIMEOUT_MS。 */
  heartbeatTimeoutMs?: number
  /** 握手参数：收到 connected 后据此发送 client_hello。 */
  handshake: HandshakeConfig
}

/** client_hello 的可配置部分（密钥/provider 由服务端持有，不下发浏览器）。 */
export type HandshakeConfig = {
  translateStrategy: Strategy
  dubbing: DubbingFlag
  responseMode?: ResponseMode
  sourceLanguage?: SourceLanguage
  targetLanguage?: TargetLanguage
  asrEngineType?: AsrEngineType
  ttsVoiceType?: TtsVoiceType
}

type MessageListener = (message: WsMessage) => void
type StatusListener = (status: WsConnectionStatus) => void
type Unsubscribe = () => void

const defaultSocketFactory = (url: string): WebSocketLike =>
  new WebSocket(url) as unknown as WebSocketLike

/** 组装带 token 的连接 URL：`{url}?token={tenantId}:{clientId}`。 */
export function buildWsUrl(
  url: string,
  tenantId: string,
  clientId: string,
): string {
  const token = `${tenantId}:${clientId}`
  return `${url}?token=${encodeURIComponent(token)}`
}

export class WsClient {
  private readonly url: string
  private readonly tenantId: string
  private readonly clientId: string
  private readonly socketFactory: (url: string) => WebSocketLike
  private readonly heartbeatIntervalMs: number
  private readonly heartbeatTimeoutMs: number
  private readonly handshake: HandshakeConfig

  private socket: WebSocketLike | null = null
  private status: WsConnectionStatus = 'idle'
  private requestSeq = 0

  private heartbeatTimer: ReturnType<typeof setInterval> | null = null
  private pongTimer: ReturnType<typeof setTimeout> | null = null

  private readonly messageListeners = new Set<MessageListener>()
  private readonly statusListeners = new Set<StatusListener>()
  /** 按 type 路由的订阅表（asr_result/translation_result/... → 回调集合）。 */
  private readonly typeListeners = new Map<WsType, Set<MessageListener>>()

  constructor(options: WsClientOptions) {
    this.url = options.url
    this.tenantId = options.tenantId
    this.clientId = options.clientId
    this.socketFactory = options.socketFactory ?? defaultSocketFactory
    this.heartbeatIntervalMs =
      options.heartbeatIntervalMs ?? HEARTBEAT_INTERVAL_MS
    this.heartbeatTimeoutMs = options.heartbeatTimeoutMs ?? HEARTBEAT_TIMEOUT_MS
    this.handshake = options.handshake
  }

  /** 当前连接状态。 */
  getStatus(): WsConnectionStatus {
    return this.status
  }

  /** 是否处于可发送状态。 */
  isOpen(): boolean {
    return this.socket?.readyState === WS_OPEN
  }

  /** 生成单调递增 requestId，可选业务前缀。 */
  nextRequestId(prefix = 'req'): string {
    this.requestSeq += 1
    return `${prefix}-${this.requestSeq}`
  }

  /** 建立连接（幂等：已连接时忽略）。 */
  connect(): void {
    if (this.socket) {
      return
    }

    const target = buildWsUrl(this.url, this.tenantId, this.clientId)
    // 不打印 token（conventions §7）：仅记录基础地址。
    console.info('[ws] connecting', this.url)
    this.setStatus('connecting')

    const socket = this.socketFactory(target)
    this.socket = socket

    socket.onopen = () => {
      console.info('[ws] open')
      this.setStatus('open')
      this.startHeartbeat()
    }
    socket.onclose = () => {
      console.info('[ws] closed')
      this.stopHeartbeat()
      this.socket = null
      this.setStatus('closed')
    }
    socket.onerror = () => {
      console.error('[ws] socket error')
    }
    socket.onmessage = (event) => {
      this.handleRawMessage(event.data)
    }
  }

  /** 关闭连接。 */
  close(): void {
    if (!this.socket) {
      return
    }
    this.stopHeartbeat()
    this.socket.close()
    this.socket = null
    this.setStatus('closed')
  }

  /** 发送一条上行消息，自动 JSON 编码。 */
  send(message: OutboundMessage): void {
    if (!this.socket || this.socket.readyState !== WS_OPEN) {
      console.warn('[ws] send skipped, socket not open:', message.type)
      return
    }
    this.socket.send(JSON.stringify(message))
  }

  /** 订阅解码后的入站消息（全量），返回退订函数。 */
  onMessage(listener: MessageListener): Unsubscribe {
    this.messageListeners.add(listener)
    return () => this.messageListeners.delete(listener)
  }

  /**
   * 按消息 type 订阅，返回退订函数。回调拿到的是已窄化的消息子类型。
   * 例：`on('asr_result', (m) => ...)` 中 m 为 AsrResultMessage。
   */
  on<T extends WsType>(
    type: T,
    listener: (message: Extract<WsMessage, { type: T }>) => void,
  ): Unsubscribe {
    let set = this.typeListeners.get(type)
    if (!set) {
      set = new Set()
      this.typeListeners.set(type, set)
    }
    const wrapped = listener as MessageListener
    set.add(wrapped)
    return () => set.delete(wrapped)
  }

  /** 订阅连接状态变化，返回退订函数。 */
  onStatusChange(listener: StatusListener): Unsubscribe {
    this.statusListeners.add(listener)
    return () => this.statusListeners.delete(listener)
  }

  private handleRawMessage(raw: unknown): void {
    if (typeof raw !== 'string') {
      console.warn('[ws] dropping non-text frame')
      return
    }

    let parsed: unknown
    try {
      parsed = JSON.parse(raw)
    } catch {
      console.warn('[ws] dropping invalid JSON frame')
      return
    }

    if (!isWsMessage(parsed)) {
      console.warn('[ws] dropping frame without string type')
      return
    }

    // 握手：收到 connected 立即发送 client_hello（A.2）。
    if (isConnected(parsed)) {
      console.info('[ws] connected', parsed.connectionId)
      this.sendClientHello()
    }

    // pong 关闭在途超时计时；不拦截分发，订阅方仍可观察到该帧。
    if (isPong(parsed)) {
      this.clearPongTimer()
    }

    // error 帧为信令级错误（conventions §9），用 error 级日志并照常分发，
    // 由上层（顶栏/编排）决定提示，不在传输层吞掉。
    if (isError(parsed)) {
      console.error('[ws] error frame:', parsed.error)
    }

    this.dispatch(parsed)
  }

  /** 先派发给全量订阅者，再派发给按 type 订阅者。 */
  private dispatch(message: WsMessage): void {
    for (const listener of this.messageListeners) {
      listener(message)
    }
    const set = this.typeListeners.get(message.type)
    if (set) {
      for (const listener of set) {
        listener(message)
      }
    }
  }

  /** 组装并发送 client_hello（A.2）。 */
  private sendClientHello(): void {
    const metadata: ClientHelloMessage['metadata'] = {
      translateStrategy: this.handshake.translateStrategy,
      dubbing: this.handshake.dubbing,
    }

    if (this.handshake.sourceLanguage) {
      metadata.sourceLanguage = this.handshake.sourceLanguage
    }
    if (this.handshake.targetLanguage) {
      metadata.targetLanguage = this.handshake.targetLanguage
    }
    if (this.handshake.asrEngineType) {
      metadata.asrEngineType = this.handshake.asrEngineType
    }
    if (this.handshake.ttsVoiceType) {
      metadata.ttsVoiceType = this.handshake.ttsVoiceType
    }

    const hello: ClientHelloMessage = {
      type: WS_TYPE.client_hello,
      requestId: this.nextRequestId('hello'),
      tenantId: this.tenantId,
      clientId: this.clientId,
      responseMode: this.handshake.responseMode ?? 'compact',
      metadata,
    }
    console.info('[ws] -> client_hello', hello.metadata.translateStrategy)
    this.send(hello)
  }

  private setStatus(status: WsConnectionStatus): void {
    if (this.status === status) {
      return
    }
    this.status = status
    for (const listener of this.statusListeners) {
      listener(status)
    }
  }

  private startHeartbeat(): void {
    this.stopHeartbeat()
    this.heartbeatTimer = setInterval(() => {
      this.sendPing()
    }, this.heartbeatIntervalMs)
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer !== null) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
    this.clearPongTimer()
  }

  private sendPing(): void {
    if (!this.isOpen()) {
      return
    }
    // 已有一次 ping 在等 pong：不再叠加计时，等待既有超时判定。
    if (this.pongTimer === null) {
      this.pongTimer = setTimeout(() => {
        this.handlePongTimeout()
      }, this.heartbeatTimeoutMs)
    }
    this.send({ type: WS_TYPE.ping, requestId: this.nextRequestId('ping') })
  }

  private clearPongTimer(): void {
    if (this.pongTimer !== null) {
      clearTimeout(this.pongTimer)
      this.pongTimer = null
    }
  }

  private handlePongTimeout(): void {
    console.warn('[ws] pong timeout, closing connection')
    this.pongTimer = null
    this.close()
  }
}
