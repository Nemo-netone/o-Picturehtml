/**
 * WS 控制协议类型定义（前端 ↔ api-server）。
 *
 * 字段逐字对齐 `docs/05-interfaces.md` A 节。所有 WS 字段访问都应经本文件的
 * getter / 类型守卫，调用方禁止裸取 `msg.metadata.xxx`（见 conventions §1.3）。
 */

// ---------------------------------------------------------------------------
// 字面量联合类型
// ---------------------------------------------------------------------------

/** 翻译策略（前端可选 + set_strategy 携带）。 */
export type Strategy = 'hybrid' | 'tmt' | 'deepseek'

/** 中文字幕来源引擎。兼容期保留旧 deepseek-flash，主链路使用 llm-tmt。 */
export type TranslationEngine = 'tmt' | 'deepseek-flash' | 'llm-tmt'

/** 回包模式，MVP 固定 compact。 */
export type ResponseMode = 'compact' | 'verbose'

/** 配音开关，metadata 内以字符串 "0"/"1" 传输。 */
export type DubbingFlag = '0' | '1'

/** 会话输入语言，也就是 ASR 识别语言。 */
export type SourceLanguage =
  | 'zh'
  | 'en'
  | 'id'
  | 'fil'
  | 'th'
  | 'pt'
  | 'tr'
  | 'ar'
  | 'es'
  | 'hi'
  | 'fr'
  | 'de'

/** 会话输出语言，也就是 TMT/LLM/TTS 目标语言。 */
export type TargetLanguage = 'zh' | 'en'

/** 腾讯 ASR EngineType，由输入语言推导。 */
export type AsrEngineType =
  | '16k_zh'
  | '16k_en'
  | '16k_id'
  | '16k_fil'
  | '16k_th'
  | '16k_pt'
  | '16k_tr'
  | '16k_ar'
  | '16k_es'
  | '16k_hi'
  | '16k_fr'
  | '16k_de'

/** 腾讯 TTS VoiceType，后端当前透传并按目标语言使用。 */
export type TtsVoiceType = string

/** WS 消息 type 字符串集合，与协议字段逐字一致（conventions §1.1）。 */
export const WS_TYPE = {
  connected: 'connected',
  client_hello: 'client_hello',
  client_hello_ack: 'client_hello_ack',
  webrtc_offer: 'webrtc_offer',
  webrtc_answer: 'webrtc_answer',
  ice: 'ice',
  asr_result: 'asr_result',
  translation_result: 'translation_result',
  asr_final: 'asr_final',
  tmt_final: 'tmt_final',
  llm_tmt_final: 'llm_tmt_final',
  set_strategy: 'set_strategy',
  set_strategy_ack: 'set_strategy_ack',
  tts_command: 'tts_command',
  tts_result: 'tts_result',
  ping: 'ping',
  pong: 'pong',
  error: 'error',
} as const

export type WsType = (typeof WS_TYPE)[keyof typeof WS_TYPE]

// ---------------------------------------------------------------------------
// 公共字段
// ---------------------------------------------------------------------------

/** 所有消息共享的可选公共字段（05-interfaces A 节「公共字段」）。 */
type CommonFields = {
  requestId?: string
  connectionId?: string
  callId?: string
  userId?: string
}

// ---------------------------------------------------------------------------
// 服务端 → 前端
// ---------------------------------------------------------------------------

/** A.1 连接建立。 */
export type ConnectedMessage = CommonFields & {
  type: typeof WS_TYPE.connected
  connectionId: string
  tenantId: string
  clientId: string
}

/** A.2 握手确认。 */
export type ClientHelloAckMessage = CommonFields & {
  type: typeof WS_TYPE.client_hello_ack
  requestId: string
  connectionId: string
  responseMode: ResponseMode
  metadata?: {
    translateStrategy?: Strategy
    requestedTranslateStrategy?: Strategy
    dubbing?: DubbingFlag
    sourceLanguage?: SourceLanguage
    targetLanguage?: TargetLanguage
    asrEngineType?: AsrEngineType
    ttsLanguage?: string
    ttsPrimaryLanguage?: string
    ttsVoiceType?: TtsVoiceType
  }
}

/** A.3 下行 SDP answer。 */
export type WebrtcAnswerMessage = CommonFields & {
  type: typeof WS_TYPE.webrtc_answer
  requestId: string
  callId: string
  userId: string
  sdp: string
}

/** A.4 旧协议英文识别结果（partial/final）；utteranceId 位于 metadata。 */
export type AsrResultMessage = CommonFields & {
  type: typeof WS_TYPE.asr_result
  callId: string
  userId: string
  text: string
  isFinal: boolean
  confidence?: number
  language?: string
  metadata: { utteranceId: string } & Record<string, unknown>
}

/** 新协议英文 ASR final；utteranceId 位于顶层。 */
export type AsrFinalMessage = CommonFields & {
  type: typeof WS_TYPE.asr_final
  callId: string
  userId: string
  utteranceId: string
  text: string
  language?: string
}

/** A.5 旧协议中文翻译结果；utteranceId 位于顶层。 */
export type TranslationResultMessage = CommonFields & {
  type: typeof WS_TYPE.translation_result
  callId: string
  userId: string
  utteranceId: string
  sourceText: string
  text: string
  isFinal: boolean
  engine: TranslationEngine
  revised: boolean
  language?: string
}

/** 新协议 TMT 快翻 final；在混合策略下仍作为待 AI 矫正草稿展示。 */
export type TmtFinalMessage = CommonFields & {
  type: typeof WS_TYPE.tmt_final
  callId: string
  userId: string
  utteranceId: string
  sourceText?: string
  text: string
  language?: string
}

/** 新协议 AI 基于 TMT 的最终矫正译文。 */
export type LlmTmtFinalMessage = CommonFields & {
  type: typeof WS_TYPE.llm_tmt_final
  callId: string
  userId: string
  utteranceId: string
  sourceText?: string
  text: string
  revised?: boolean
  language?: string
}

/** A.6 策略切换确认。 */
export type SetStrategyAckMessage = CommonFields & {
  type: typeof WS_TYPE.set_strategy_ack
  requestId: string
  metadata: { translateStrategy: Strategy }
}

/** A.7 TTS 进入下行队列确认。 */
export type TtsResultMessage = CommonFields & {
  type: typeof WS_TYPE.tts_result
  requestId: string
  callId: string
  format: string
  sampleRate: number
  isLast: boolean
  metadata?: Record<string, unknown>
}

/** A.8 心跳回包。 */
export type PongMessage = CommonFields & {
  type: typeof WS_TYPE.pong
  requestId: string
  connectionId: string
}

/** A.8 错误回包（信令/协议级）。 */
export type ErrorMessage = CommonFields & {
  type: typeof WS_TYPE.error
  requestId?: string
  callId?: string
  error: string
}

// ---------------------------------------------------------------------------
// 前端 → 服务端
// ---------------------------------------------------------------------------

/** A.2 握手请求。 */
export type ClientHelloMessage = CommonFields & {
  type: typeof WS_TYPE.client_hello
  requestId: string
  tenantId: string
  clientId: string
  responseMode: ResponseMode
  metadata: {
    translateStrategy: Strategy
    dubbing: DubbingFlag
    sourceLanguage?: SourceLanguage
    targetLanguage?: TargetLanguage
    asrEngineType?: AsrEngineType
    ttsVoiceType?: TtsVoiceType
  }
}

/** A.3 上行 SDP offer。 */
export type WebrtcOfferMessage = CommonFields & {
  type: typeof WS_TYPE.webrtc_offer
  requestId: string
  callId: string
  userId: string
  sdp: string
}

/** A.3 ICE candidate 交换（双向）。candidate 为序列化 JSON 字符串。 */
export type IceMessage = CommonFields & {
  type: typeof WS_TYPE.ice
  callId: string
  userId: string
  candidate: string
}

/** A.6 运行时切换翻译策略。 */
export type SetStrategyMessage = CommonFields & {
  type: typeof WS_TYPE.set_strategy
  requestId: string
  metadata: { translateStrategy: Strategy }
}

/** A.7 显式请求配音（调试用，正式链路由服务端主动触发）。 */
export type TtsCommandMessage = CommonFields & {
  type: typeof WS_TYPE.tts_command
  requestId: string
  callId: string
  userId: string
  text: string
  voice?: string
  language?: string
}

/** A.8 心跳。 */
export type PingMessage = CommonFields & {
  type: typeof WS_TYPE.ping
  requestId: string
}

// ---------------------------------------------------------------------------
// 联合类型
// ---------------------------------------------------------------------------

/** 服务端可能下发的全部消息。 */
export type InboundMessage =
  | ConnectedMessage
  | ClientHelloAckMessage
  | WebrtcAnswerMessage
  | IceMessage
  | AsrResultMessage
  | AsrFinalMessage
  | TranslationResultMessage
  | TmtFinalMessage
  | LlmTmtFinalMessage
  | SetStrategyAckMessage
  | TtsResultMessage
  | PongMessage
  | ErrorMessage

/** 前端可能上行的全部消息。 */
export type OutboundMessage =
  | ClientHelloMessage
  | WebrtcOfferMessage
  | IceMessage
  | SetStrategyMessage
  | TtsCommandMessage
  | PingMessage

/** 协议内任意一条消息。 */
export type WsMessage = InboundMessage | OutboundMessage

// ---------------------------------------------------------------------------
// 字幕行模型（state/subtitles 与组件渲染依据）
// ---------------------------------------------------------------------------

/**
 * 单句双语字幕行，以 utteranceId 为主键就地更新。
 * - en/zh 为当前文本，*Final 表示是否锁定（黑字）。
 * - revised 表示 AI 矫正与 TMT 快翻不同，需高亮「✦已纠正」。
 */
export type SubtitleLine = {
  utteranceId: string
  en: string
  enFinal: boolean
  zh: string
  zhFinal: boolean
  revised: boolean
  /** 最近一次中文译文来源引擎，便于调试/降级判断。 */
  engine?: TranslationEngine
  /** 行创建顺序，用于稳定排序与「保留最近 N 条」裁剪。 */
  seq: number
}

// ---------------------------------------------------------------------------
// 类型守卫
// ---------------------------------------------------------------------------

/** 是否为协议对象（含字符串 type 字段）。 */
export function isWsMessage(value: unknown): value is WsMessage {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { type?: unknown }).type === 'string'
  )
}

export function isConnected(msg: WsMessage): msg is ConnectedMessage {
  return msg.type === WS_TYPE.connected
}

export function isClientHelloAck(msg: WsMessage): msg is ClientHelloAckMessage {
  return msg.type === WS_TYPE.client_hello_ack
}

export function isWebrtcAnswer(msg: WsMessage): msg is WebrtcAnswerMessage {
  return msg.type === WS_TYPE.webrtc_answer
}

export function isIce(msg: WsMessage): msg is IceMessage {
  return msg.type === WS_TYPE.ice
}

export function isAsrResult(msg: WsMessage): msg is AsrResultMessage {
  return msg.type === WS_TYPE.asr_result
}

export function isAsrFinal(msg: WsMessage): msg is AsrFinalMessage {
  return msg.type === WS_TYPE.asr_final
}

export function isTranslationResult(
  msg: WsMessage,
): msg is TranslationResultMessage {
  return msg.type === WS_TYPE.translation_result
}

export function isTmtFinal(msg: WsMessage): msg is TmtFinalMessage {
  return msg.type === WS_TYPE.tmt_final
}

export function isLlmTmtFinal(msg: WsMessage): msg is LlmTmtFinalMessage {
  return msg.type === WS_TYPE.llm_tmt_final
}

export function isSetStrategyAck(msg: WsMessage): msg is SetStrategyAckMessage {
  return msg.type === WS_TYPE.set_strategy_ack
}

export function isTtsResult(msg: WsMessage): msg is TtsResultMessage {
  return msg.type === WS_TYPE.tts_result
}

export function isPong(msg: WsMessage): msg is PongMessage {
  return msg.type === WS_TYPE.pong
}

export function isError(msg: WsMessage): msg is ErrorMessage {
  return msg.type === WS_TYPE.error
}

// ---------------------------------------------------------------------------
// 字段 getter（屏蔽 asr/translation 的 utteranceId 位置差异等）
// ---------------------------------------------------------------------------

/**
 * 统一取 utteranceId：
 * - asr_result：位于 `metadata.utteranceId`
 * - translation_result / asr_final / tmt_final / llm_tmt_final：位于顶层
 * 其它消息无该字段，返回 undefined（对应 mvp 风险项「字段位置不一致」）。
 */
export function getUtteranceId(msg: WsMessage): string | undefined {
  if (isAsrResult(msg)) {
    return msg.metadata.utteranceId
  }
  if (
    isTranslationResult(msg) ||
    isAsrFinal(msg) ||
    isTmtFinal(msg) ||
    isLlmTmtFinal(msg)
  ) {
    return msg.utteranceId
  }
  return undefined
}

/** translation_result 是否来自旧 DeepSeek 纠错（黑字锁定来源）。 */
export function isDeepseekTranslation(msg: TranslationResultMessage): boolean {
  return msg.engine === 'deepseek-flash'
}

/** 中文字幕是否已来自 AI 最终矫正结果。 */
export function isAiCorrectedEngine(
  engine: TranslationEngine | undefined,
): boolean {
  return engine === 'llm-tmt' || engine === 'deepseek-flash'
}
