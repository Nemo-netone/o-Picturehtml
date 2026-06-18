import type { ReactNode } from 'react'

type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'disconnected'

type TopBarProps = {
  status: ConnectionStatus
  latencyLabel: string
  sessionId: string
  serviceUrl: string
  actions?: ReactNode
}

const STATUS_LABEL: Record<ConnectionStatus, string> = {
  idle: '未连接',
  connecting: '连接中',
  connected: '已连接',
  disconnected: '已断开',
}

export function TopBar({
  status,
  latencyLabel,
  sessionId,
  serviceUrl,
  actions,
}: TopBarProps) {
  return (
    <header className="top-bar">
      <div className="bar-inner">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            SS
          </span>
          <div>
            <h1>SimulSpeak 同声传译</h1>
            <p>{serviceUrl}</p>
          </div>
        </div>

        <div className="top-bar-actions">
          <dl className="status-list" aria-label="会话状态">
            <div className="status-item">
              <dt>连接</dt>
              <dd className={`connection-state connection-state-${status}`}>
                <span aria-hidden="true" />
                {STATUS_LABEL[status]}
              </dd>
            </div>
            <div className="status-item">
              <dt>延迟</dt>
              <dd>{latencyLabel}</dd>
            </div>
            <div className="status-item">
              <dt>会话</dt>
              <dd>{sessionId}</dd>
            </div>
          </dl>
          {actions ? <div className="top-bar-extra">{actions}</div> : null}
        </div>
      </div>
    </header>
  )
}
