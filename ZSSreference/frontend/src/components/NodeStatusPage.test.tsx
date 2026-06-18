import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { NodeStatusContent } from './NodeStatusPage'
import type { SystemNodesResult } from '../api/system'

const emptyData: SystemNodesResult = {
  refreshedAt: '2026-06-07T00:00:00Z',
  media: {
    summary: {
      total: 0,
      available: 0,
      unavailable: 0,
      up: 0,
      down: 0,
      draining: 0,
      suspect: 0,
      capacity: 0,
      currentCalls: 0,
    },
    nodes: [],
  },
  workers: {
    summary: {
      total: 0,
      available: 0,
      unavailable: 0,
      up: 0,
      down: 0,
      draining: 0,
      suspect: 0,
      capacity: 0,
      activeTasks: 0,
    },
    nodes: [],
  },
}

const populatedData: SystemNodesResult = {
  refreshedAt: '2026-06-07T00:00:00Z',
  media: {
    summary: {
      total: 1,
      available: 1,
      unavailable: 0,
      up: 1,
      down: 0,
      draining: 0,
      suspect: 0,
      capacity: 100,
      currentCalls: 12,
    },
    nodes: [
      {
        id: 'media-a',
        type: 'media',
        endpoint: 'ws://127.0.0.1:8081/pbx/ws',
        status: 'up',
        maxCalls: 100,
        currentCalls: 12,
        capabilities: ['vad', 'asr'],
        startedAt: '2026-06-07T00:00:00Z',
      },
    ],
  },
  workers: {
    summary: {
      total: 1,
      available: 1,
      unavailable: 0,
      up: 1,
      down: 0,
      draining: 0,
      suspect: 0,
      capacity: 4,
      activeTasks: 2,
    },
    nodes: [
      {
        id: 'worker-a',
        type: 'worker',
        endpoint: 'worker://worker-a',
        status: 'up',
        maxCalls: 4,
        currentCalls: 2,
        capabilities: ['vocabulary'],
        startedAt: '2026-06-07T00:00:00Z',
      },
    ],
  },
}

describe('NodeStatusContent', () => {
  it('renders loading and error states', () => {
    const loading = renderToStaticMarkup(
      <NodeStatusContent
        isLoading
        errorMessage=""
        onRefresh={() => undefined}
      />,
    )
    expect(loading).toContain('加载节点状态')

    const error = renderToStaticMarkup(
      <NodeStatusContent
        isLoading={false}
        errorMessage="registry unavailable"
        onRefresh={() => undefined}
      />,
    )
    expect(error).toContain('registry unavailable')
    expect(error).toContain('role="alert"')
  })

  it('renders empty media and worker groups', () => {
    const html = renderToStaticMarkup(
      <NodeStatusContent
        data={emptyData}
        isLoading={false}
        errorMessage=""
        onRefresh={() => undefined}
      />,
    )

    expect(html).toContain('暂无 PBX 节点')
    expect(html).toContain('暂无 Worker 节点')
  })

  it('renders media and worker node details', () => {
    const html = renderToStaticMarkup(
      <NodeStatusContent
        data={populatedData}
        isLoading={false}
        errorMessage=""
        onRefresh={() => undefined}
      />,
    )

    expect(html).toContain('media-a')
    expect(html).toContain('ws://127.0.0.1:8081/pbx/ws')
    expect(html).toContain('worker-a')
    expect(html).toContain('worker://worker-a')
    expect(html).toContain('vocabulary')
    expect(html).toContain('任务 2 / 4')
  })
})
