import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  createSystemApiClient,
  type MediaNodeSummary,
  type SystemNode,
  type SystemNodesResult,
  type WorkerNodeSummary,
} from '../api/system'
import { appConfig } from '../config'

const NODE_REFRESH_INTERVAL_MS = 5000

function messageFromError(error: unknown) {
  return error instanceof Error && error.message
    ? error.message
    : '请求失败，请稍后重试。'
}

function formatDateTime(value?: string) {
  if (!value) {
    return '--'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function statusLabel(status?: string) {
  if (status === 'up') {
    return '在线'
  }
  if (status === 'down') {
    return '离线'
  }
  if (status === 'draining') {
    return '排空'
  }
  if (status === 'suspect') {
    return '疑似异常'
  }
  return status || '--'
}

function SummaryItem({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="nodes-summary-item">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function MediaSummary({ summary }: { summary: MediaNodeSummary }) {
  return (
    <div className="nodes-summary-grid" aria-label="PBX 节点摘要">
      <SummaryItem label="总数" value={summary.total} />
      <SummaryItem label="可用" value={summary.available} />
      <SummaryItem label="通话" value={`${summary.currentCalls} / ${summary.capacity}`} />
      <SummaryItem label="排空" value={summary.draining} />
    </div>
  )
}

function WorkerSummary({ summary }: { summary: WorkerNodeSummary }) {
  return (
    <div className="nodes-summary-grid" aria-label="Worker 节点摘要">
      <SummaryItem label="总数" value={summary.total} />
      <SummaryItem label="可用" value={summary.available} />
      <SummaryItem label="任务" value={`${summary.activeTasks} / ${summary.capacity}`} />
      <SummaryItem label="异常" value={summary.suspect + summary.down} />
    </div>
  )
}

function NodeCard({
  node,
  loadLabel,
}: {
  node: SystemNode
  loadLabel: string
}) {
  const capabilities = node.capabilities?.length
    ? node.capabilities.join(', ')
    : '--'

  return (
    <article className="nodes-card">
      <header className="nodes-card-head">
        <strong>{node.id}</strong>
        <span className={`nodes-status nodes-status-${node.status}`}>
          {statusLabel(node.status)}
        </span>
      </header>
      <dl className="nodes-meta-grid">
        <div>
          <dt>Endpoint</dt>
          <dd>{node.endpoint || '--'}</dd>
        </div>
        <div>
          <dt>负载</dt>
          <dd>{loadLabel}</dd>
        </div>
        <div>
          <dt>启动</dt>
          <dd>{formatDateTime(node.startedAt)}</dd>
        </div>
        <div>
          <dt>能力</dt>
          <dd>{capabilities}</dd>
        </div>
      </dl>
    </article>
  )
}

function NodeGroup({
  title,
  subtitle,
  emptyText,
  nodes,
  summary,
  loadName,
  kind,
}: {
  title: string
  subtitle: string
  emptyText: string
  nodes: SystemNode[]
  summary: MediaNodeSummary | WorkerNodeSummary
  loadName: string
  kind: 'media' | 'worker'
}) {
  return (
    <section className="nodes-panel">
      <div className="nodes-section-heading">
        <h2>{title}</h2>
        <span>{subtitle}</span>
      </div>
      {kind === 'media' ? (
        <MediaSummary summary={summary as MediaNodeSummary} />
      ) : (
        <WorkerSummary summary={summary as WorkerNodeSummary} />
      )}
      <div className="nodes-list">
        {nodes.length > 0 ? (
          nodes.map((node) => (
            <NodeCard
              key={`${node.type}-${node.id}`}
              node={node}
              loadLabel={`${loadName} ${node.currentCalls} / ${node.maxCalls || '--'}`}
            />
          ))
        ) : (
          <p className="nodes-state-message">{emptyText}</p>
        )}
      </div>
    </section>
  )
}

export function NodeStatusContent({
  data,
  isLoading,
  errorMessage,
  onRefresh,
}: {
  data?: SystemNodesResult
  isLoading: boolean
  errorMessage?: string
  onRefresh: () => void
}) {
  return (
    <section className="nodes-workspace" aria-label="节点状态">
      <div className="nodes-toolbar">
        <div>
          <h2>节点状态</h2>
          <span>
            {data?.refreshedAt
              ? `刷新于 ${formatDateTime(data.refreshedAt)}`
              : '每 5 秒自动刷新'}
          </span>
        </div>
        <button
          type="button"
          className="secondary-action nodes-refresh-button"
          disabled={isLoading}
          onClick={onRefresh}
        >
          {isLoading ? '刷新中' : '刷新'}
        </button>
      </div>

      {errorMessage ? (
        <p className="nodes-state-message" role="alert">
          {errorMessage}
        </p>
      ) : null}

      {isLoading && !data ? (
        <p className="nodes-state-message">加载节点状态...</p>
      ) : null}

      {data ? (
        <div className="nodes-grid">
          <NodeGroup
            title="PBX 节点"
            subtitle="媒体接入与通话负载"
            emptyText="暂无 PBX 节点。"
            nodes={data.media.nodes}
            summary={data.media.summary}
            loadName="通话"
            kind="media"
          />
          <NodeGroup
            title="Worker 节点"
            subtitle="异步任务处理能力"
            emptyText="暂无 Worker 节点。"
            nodes={data.workers.nodes}
            summary={data.workers.summary}
            loadName="任务"
            kind="worker"
          />
        </div>
      ) : null}
    </section>
  )
}

export function NodeStatusPage() {
  const client = useMemo(
    () => createSystemApiClient({ baseUrl: appConfig.apiHttpUrl }),
    [],
  )
  const [data, setData] = useState<SystemNodesResult>()
  const [isLoading, setIsLoading] = useState(true)
  const [errorMessage, setErrorMessage] = useState('')

  const loadNodes = useCallback(
    async (silent = false) => {
      if (!silent) {
        setIsLoading(true)
      }

      try {
        const nextData = await client.getNodes()
        setData(nextData)
        setErrorMessage('')
      } catch (error) {
        setErrorMessage(messageFromError(error))
      } finally {
        if (!silent) {
          setIsLoading(false)
        }
      }
    },
    [client],
  )

  useEffect(() => {
    const initialTimer = window.setTimeout(() => {
      void loadNodes()
    }, 0)
    const refreshTimer = window.setInterval(() => {
      void loadNodes(true)
    }, NODE_REFRESH_INTERVAL_MS)

    return () => {
      window.clearTimeout(initialTimer)
      window.clearInterval(refreshTimer)
    }
  }, [loadNodes])

  return (
    <NodeStatusContent
      data={data}
      isLoading={isLoading}
      errorMessage={errorMessage}
      onRefresh={() => void loadNodes()}
    />
  )
}
