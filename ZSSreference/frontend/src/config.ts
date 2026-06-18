const readStringEnv = (key: keyof ImportMetaEnv, fallback: string) => {
  const value = import.meta.env[key]

  return typeof value === 'string' && value.trim() ? value.trim() : fallback
}

export const THROTTLE_MS = 180
export const MAX_SUBTITLE_LINES = 50

/** 心跳 ping 发送间隔（ms）。 */
export const HEARTBEAT_INTERVAL_MS = 15000
/** 发出 ping 后等待 pong 的超时阈值（ms），超时判定连接已断开。 */
export const HEARTBEAT_TIMEOUT_MS = 10000

export const PIP_WINDOW_WIDTH = 560
export const PIP_WINDOW_HEIGHT = 320

/** WebRTC 默认 STUN 配置；TURN/自定义 ICE 服务后置到联调阶段。 */
export const RTC_ICE_SERVERS: RTCIceServer[] = [
  { urls: 'stun:stun.l.google.com:19302' },
]

const wsUrl = readStringEnv('VITE_WS_URL', 'ws://localhost:8080/ws')

export const appConfig = {
  wsUrl,
  apiHttpUrl: readStringEnv('VITE_API_HTTP_URL', ''),
  tenantId: readStringEnv('VITE_TENANT_ID', 'demo-tenant'),
  clientId: readStringEnv('VITE_CLIENT_ID', 'local-client'),
} as const
