import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from 'react'

import {
  createHistoryApiClient,
  type InterpreterASRCallback,
  type InterpreterSessionDetailResult,
  type InterpreterSessionListResult,
  type InterpreterSessionSummary,
  type VocabularyEntryResult,
  type VocabularyTaskDetailResult,
  type VocabularyTaskResult,
} from '../api/history'
import { appConfig } from '../config'
import {
  DEFAULT_MAX_WORDS,
  HISTORY_PAGE_LIMIT,
  SESSION_STATE_OPTIONS,
  TASK_LIST_LIMIT,
  TASK_POLL_INTERVAL_MS,
  buildSessionListQuery,
  canGoNextPage,
  getDefaultSelectedSessionId,
  isVocabularyTaskTerminal,
  nextOffset,
  previousOffset,
  validateMaxWords,
  type SessionStateFilter,
} from '../history/historyModel'
import { CustomSelect } from './CustomSelect'

const emptySessionList: InterpreterSessionListResult = {
  items: [],
  total: 0,
  limit: HISTORY_PAGE_LIMIT,
  offset: 0,
}

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

function stateLabel(state?: string) {
  if (state === 'active') {
    return '进行中'
  }
  if (state === 'ended') {
    return '已结束'
  }
  if (state === 'failed') {
    return '失败'
  }
  return state || '--'
}

function taskStatusLabel(status?: string) {
  if (status === 'pending') {
    return '等待中'
  }
  if (status === 'running') {
    return '生成中'
  }
  if (status === 'succeeded') {
    return '已完成'
  }
  if (status === 'failed') {
    return '失败'
  }
  if (status === 'cancelled') {
    return '已取消'
  }
  return status || '--'
}

function finalLabel(isFinal: boolean) {
  return isFinal ? 'final' : 'partial'
}

function formatSourceUtteranceIds(value: unknown) {
  if (Array.isArray(value)) {
    return value.map(String).join(', ')
  }

  return typeof value === 'string' ? value : ''
}

function SessionListItem({
  session,
  isSelected,
  onSelect,
}: {
  session: InterpreterSessionSummary
  isSelected: boolean
  onSelect: (id: string) => void
}) {
  return (
    <button
      type="button"
      className="history-session-item"
      data-selected={isSelected ? 'true' : 'false'}
      onClick={() => onSelect(session.id)}
    >
      <span className="history-session-item-main">
        <strong>{session.id}</strong>
        <span>{formatDateTime(session.startedAt || session.createdAt)}</span>
      </span>
      <span className="history-session-item-meta">
        <span>{stateLabel(session.state)}</span>
        <span>{session.translateStrategy || '--'}</span>
        <span>{session.dubbingEnabled ? '配音' : '静音'}</span>
      </span>
    </button>
  )
}

function SessionMeta({ session }: { session: InterpreterSessionSummary }) {
  const providerText = session.providerIds
    ? Object.entries(session.providerIds)
        .map(([key, ids]) => `${key}:${ids.join('/')}`)
        .join('  ')
    : '--'

  return (
    <dl className="history-meta-grid">
      <div>
        <dt>租户</dt>
        <dd>{session.tenantId || '--'}</dd>
      </div>
      <div>
        <dt>用户</dt>
        <dd>{session.userId || '--'}</dd>
      </div>
      <div>
        <dt>状态</dt>
        <dd>{stateLabel(session.state)}</dd>
      </div>
      <div>
        <dt>媒体</dt>
        <dd>{stateLabel(session.mediaState)}</dd>
      </div>
      <div>
        <dt>策略</dt>
        <dd>{session.translateStrategy || '--'}</dd>
      </div>
      <div>
        <dt>配音</dt>
        <dd>{session.dubbingEnabled ? '开启' : '关闭'}</dd>
      </div>
      <div>
        <dt>开始</dt>
        <dd>{formatDateTime(session.startedAt || session.createdAt)}</dd>
      </div>
      <div>
        <dt>结束</dt>
        <dd>{formatDateTime(session.endedAt)}</dd>
      </div>
      <div className="history-meta-wide">
        <dt>Provider</dt>
        <dd>{providerText}</dd>
      </div>
    </dl>
  )
}

function TranslationRecords({ callback }: { callback: InterpreterASRCallback }) {
  const mtRecords = callback.mtTranslations ?? []
  const llmRecords = callback.llmRevisions ?? []

  if (mtRecords.length === 0 && llmRecords.length === 0) {
    return null
  }

  return (
    <div className="history-translation-grid">
      {mtRecords.map((record) => (
        <div key={`mt-${record.id}`} className="history-translation-block">
          <span className="history-chip">TMT {record.status || '--'}</span>
          <p>{record.targetText || record.errorMessage || '--'}</p>
        </div>
      ))}
      {llmRecords.map((record) => (
        <div key={`llm-${record.id}`} className="history-translation-block">
          <span className="history-chip">
            LLM {record.revised ? '已纠正' : '未改写'}
          </span>
          <p>{record.revisedText || record.errorMessage || '--'}</p>
        </div>
      ))}
    </div>
  )
}

function ASRCallbackBlock({ callback }: { callback: InterpreterASRCallback }) {
  return (
    <article className="history-asr-block">
      <div className="history-asr-head">
        <span className="history-chip">{finalLabel(callback.isFinal)}</span>
        <span>{callback.language || 'unknown'}</span>
        <span>#{callback.sequenceNo}</span>
        <span>{formatDateTime(callback.receivedAt)}</span>
      </div>
      <p className="history-asr-text">{callback.text || '--'}</p>
      <TranslationRecords callback={callback} />
    </article>
  )
}

function SessionDetail({
  detail,
  isLoading,
  errorMessage,
}: {
  detail?: InterpreterSessionDetailResult
  isLoading: boolean
  errorMessage?: string
}) {
  if (isLoading) {
    return <p className="history-state-message">加载会话明细...</p>
  }

  if (errorMessage) {
    return (
      <p className="history-state-message" role="alert">
        {errorMessage}
      </p>
    )
  }

  if (!detail) {
    return <p className="history-state-message">请选择一条历史会话。</p>
  }

  return (
    <div className="history-detail-stack">
      <section className="history-section">
        <div className="history-section-heading">
          <h2>{detail.session.id}</h2>
          <span>{detail.utterances.length} 句</span>
        </div>
        <SessionMeta session={detail.session} />
      </section>

      <section className="history-section">
        <div className="history-section-heading">
          <h3>字幕明细</h3>
          <span>ASR / TMT / LLM</span>
        </div>
        {detail.utterances.length > 0 ? (
          <div className="history-utterance-list">
            {detail.utterances.map((utterance) => (
              <article
                key={utterance.utteranceId}
                className="history-utterance-card"
              >
                <header>
                  <strong>{utterance.utteranceId}</strong>
                  <span>{utterance.asrCallbacks.length} 帧</span>
                </header>
                {utterance.asrCallbacks.map((callback) => (
                  <ASRCallbackBlock key={callback.id} callback={callback} />
                ))}
              </article>
            ))}
          </div>
        ) : (
          <p className="history-state-message">暂无字幕明细。</p>
        )}
      </section>
    </div>
  )
}

function TaskList({
  tasks,
  activeTaskId,
  onSelectTask,
}: {
  tasks: VocabularyTaskResult[]
  activeTaskId?: string
  onSelectTask: (taskId: string) => void
}) {
  if (tasks.length === 0) {
    return <p className="history-state-message">暂无单词本任务。</p>
  }

  return (
    <div className="history-task-list">
      {tasks.map((task) => (
        <button
          key={task.id}
          type="button"
          className="history-task-item"
          data-selected={task.id === activeTaskId ? 'true' : 'false'}
          onClick={() => onSelectTask(task.id)}
        >
          <span>
            <strong>{taskStatusLabel(task.status)}</strong>
            <em>{task.maxWords} 词</em>
          </span>
          <small>{formatDateTime(task.createdAt)}</small>
        </button>
      ))}
    </div>
  )
}

function VocabularyEntries({ entries }: { entries: VocabularyEntryResult[] }) {
  if (entries.length === 0) {
    return <p className="history-state-message">任务完成后词条会显示在这里。</p>
  }

  return (
    <div className="history-entry-list">
      {entries.map((entry) => (
        <article key={`${entry.ordinal}-${entry.word}`} className="history-entry">
          <header>
            <strong>{entry.word}</strong>
            <span>{entry.difficulty || entry.partOfSpeech || '--'}</span>
          </header>
          <p>{entry.meaningZh || '--'}</p>
          {entry.exampleEn ? <blockquote>{entry.exampleEn}</blockquote> : null}
          {entry.exampleZh ? <small>{entry.exampleZh}</small> : null}
          {entry.sourceUtteranceIds ? (
            <small>{formatSourceUtteranceIds(entry.sourceUtteranceIds)}</small>
          ) : null}
        </article>
      ))}
    </div>
  )
}

function VocabularyTaskPanel({
  selectedSessionId,
  tasks,
  activeTaskId,
  taskDetail,
  isLoadingTasks,
  isLoadingDetail,
  isCreatingTask,
  errorMessage,
  maxWords,
  onMaxWordsChange,
  onCreateTask,
  onRefreshTasks,
  onSelectTask,
}: {
  selectedSessionId?: string
  tasks: VocabularyTaskResult[]
  activeTaskId?: string
  taskDetail?: VocabularyTaskDetailResult
  isLoadingTasks: boolean
  isLoadingDetail: boolean
  isCreatingTask: boolean
  errorMessage?: string
  maxWords: string
  onMaxWordsChange: (value: string) => void
  onCreateTask: (event: FormEvent<HTMLFormElement>) => void
  onRefreshTasks: () => void
  onSelectTask: (taskId: string) => void
}) {
  const entries = taskDetail?.entries ?? []

  return (
    <section className="history-section history-task-panel">
      <div className="history-section-heading">
        <h3>单词本总结</h3>
        <span>{selectedSessionId || '--'}</span>
      </div>

      <form className="history-task-form" onSubmit={onCreateTask}>
        <label className="field">
          单词数
          <input
            type="number"
            min="1"
            max="100"
            value={maxWords}
            disabled={!selectedSessionId || isCreatingTask}
            onChange={(event) => onMaxWordsChange(event.target.value)}
          />
        </label>
        <button
          type="submit"
          className="primary-action"
          disabled={!selectedSessionId || isCreatingTask}
        >
          {isCreatingTask ? '创建中' : '生成'}
        </button>
        <button
          type="button"
          className="secondary-action"
          disabled={!selectedSessionId || isLoadingTasks}
          onClick={onRefreshTasks}
        >
          刷新
        </button>
      </form>

      {errorMessage ? (
        <p className="history-state-message" role="alert">
          {errorMessage}
        </p>
      ) : null}

      {isLoadingTasks ? (
        <p className="history-state-message">加载任务...</p>
      ) : (
        <TaskList
          tasks={tasks}
          activeTaskId={activeTaskId}
          onSelectTask={onSelectTask}
        />
      )}

      <div className="history-task-detail">
        <div className="history-task-detail-head">
          <strong>{taskStatusLabel(taskDetail?.task.status)}</strong>
          <span>
            {isLoadingDetail
              ? '同步中'
              : taskDetail?.task.finishedAt
                ? formatDateTime(taskDetail.task.finishedAt)
                : '--'}
          </span>
        </div>
        <VocabularyEntries entries={entries} />
      </div>
    </section>
  )
}

export function HistoryPage() {
  const api = useMemo(
    () => createHistoryApiClient({ baseUrl: appConfig.apiHttpUrl }),
    [],
  )
  const [tenantInput, setTenantInput] = useState(appConfig.tenantId)
  const [stateInput, setStateInput] = useState<SessionStateFilter>('')
  const [queryTenantId, setQueryTenantId] = useState(appConfig.tenantId)
  const [queryState, setQueryState] = useState<SessionStateFilter>('')
  const [offset, setOffset] = useState(0)
  const [sessionList, setSessionList] = useState(emptySessionList)
  const [sessionsLoading, setSessionsLoading] = useState(false)
  const [sessionsError, setSessionsError] = useState<string>()
  const [selectedSessionId, setSelectedSessionId] = useState<string>()
  const [detail, setDetail] = useState<InterpreterSessionDetailResult>()
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string>()
  const [tasks, setTasks] = useState<VocabularyTaskResult[]>([])
  const [tasksLoading, setTasksLoading] = useState(false)
  const [activeTaskId, setActiveTaskId] = useState<string>()
  const [taskDetail, setTaskDetail] = useState<VocabularyTaskDetailResult>()
  const [taskDetailLoading, setTaskDetailLoading] = useState(false)
  const [taskError, setTaskError] = useState<string>()
  const [maxWords, setMaxWords] = useState(String(DEFAULT_MAX_WORDS))
  const [creatingTask, setCreatingTask] = useState(false)

  const loadSessions = useCallback(async () => {
    setSessionsLoading(true)
    setSessionsError(undefined)

    try {
      const result = await api.listSessions(
        buildSessionListQuery({
          tenantId: queryTenantId,
          state: queryState,
          offset,
        }),
      )
      setSessionList(result)
      setSelectedSessionId((current) =>
        getDefaultSelectedSessionId(result.items, current),
      )
    } catch (error) {
      setSessionsError(messageFromError(error))
      setSessionList(emptySessionList)
      setSelectedSessionId(undefined)
    } finally {
      setSessionsLoading(false)
    }
  }, [api, offset, queryState, queryTenantId])

  const loadDetail = useCallback(
    async (sessionId: string) => {
      setDetailLoading(true)
      setDetailError(undefined)

      try {
        setDetail(await api.getSessionDetail(sessionId))
      } catch (error) {
        setDetail(undefined)
        setDetailError(messageFromError(error))
      } finally {
        setDetailLoading(false)
      }
    },
    [api],
  )

  const loadTasks = useCallback(
    async (sessionId: string, preferredTaskId?: string) => {
      setTasksLoading(true)
      setTaskError(undefined)

      try {
        const result = await api.listVocabularyTasks(sessionId, {
          limit: TASK_LIST_LIMIT,
          offset: 0,
        })
        setTasks(result.items)
        setActiveTaskId((current) => {
          if (preferredTaskId) {
            return preferredTaskId
          }
          if (current && result.items.some((task) => task.id === current)) {
            return current
          }
          return result.items[0]?.id
        })
        if (result.items.length === 0) {
          setTaskDetail(undefined)
        }
      } catch (error) {
        setTasks([])
        setActiveTaskId(undefined)
        setTaskDetail(undefined)
        setTaskError(messageFromError(error))
      } finally {
        setTasksLoading(false)
      }
    },
    [api],
  )

  const loadTaskDetail = useCallback(
    async (taskId: string) => {
      setTaskDetailLoading(true)
      setTaskError(undefined)

      try {
        setTaskDetail(await api.getVocabularyTask(taskId))
      } catch (error) {
        setTaskError(messageFromError(error))
      } finally {
        setTaskDetailLoading(false)
      }
    },
    [api],
  )

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadSessions()
    }, 0)

    return () => window.clearTimeout(timer)
  }, [loadSessions])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (!selectedSessionId) {
        setDetail(undefined)
        setTasks([])
        setActiveTaskId(undefined)
        setTaskDetail(undefined)
        return
      }

      void loadDetail(selectedSessionId)
      void loadTasks(selectedSessionId)
    }, 0)

    return () => window.clearTimeout(timer)
  }, [loadDetail, loadTasks, selectedSessionId])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (!activeTaskId) {
        setTaskDetail(undefined)
        return
      }

      void loadTaskDetail(activeTaskId)
    }, 0)

    return () => window.clearTimeout(timer)
  }, [activeTaskId, loadTaskDetail])

  useEffect(() => {
    if (!activeTaskId) {
      return
    }

    const activeTask =
      taskDetail?.task.id === activeTaskId
        ? taskDetail.task
        : tasks.find((task) => task.id === activeTaskId)

    if (!activeTask || isVocabularyTaskTerminal(activeTask.status)) {
      return
    }

    const timer = window.setTimeout(() => {
      void loadTaskDetail(activeTaskId)
      if (selectedSessionId) {
        void loadTasks(selectedSessionId, activeTaskId)
      }
    }, TASK_POLL_INTERVAL_MS)

    return () => window.clearTimeout(timer)
  }, [
    activeTaskId,
    loadTaskDetail,
    loadTasks,
    selectedSessionId,
    taskDetail,
    tasks,
  ])

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setOffset(0)
    setQueryTenantId(tenantInput)
    setQueryState(stateInput)
  }

  const handleCreateTask = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()

    if (!selectedSessionId) {
      return
    }

    const validation = validateMaxWords(maxWords)
    if ('error' in validation) {
      setTaskError(validation.error)
      return
    }

    setCreatingTask(true)
    setTaskError(undefined)

    try {
      const task = await api.createVocabularyTask(
        selectedSessionId,
        validation.value,
      )
      setActiveTaskId(task.id)
      setTaskDetail({ task, entries: [] })
      await loadTasks(selectedSessionId, task.id)
    } catch (error) {
      setTaskError(messageFromError(error))
    } finally {
      setCreatingTask(false)
    }
  }

  const selectedSession = sessionList.items.find(
    (session) => session.id === selectedSessionId,
  )
  const hasNextPage = canGoNextPage(
    sessionList.total,
    sessionList.offset,
    sessionList.limit,
  )

  return (
    <section className="history-workspace" aria-label="历史会话">
      <aside className="history-sidebar">
        <div className="history-panel-heading">
          <h2>历史会话</h2>
          <span>{appConfig.apiHttpUrl || '同源 /api'}</span>
        </div>

        <form className="history-filters" onSubmit={handleSearch}>
          <label className="field">
            租户
            <input
              type="text"
              value={tenantInput}
              onChange={(event) => setTenantInput(event.target.value)}
            />
          </label>
          <label className="field">
            状态
            <CustomSelect
              value={stateInput}
              options={SESSION_STATE_OPTIONS}
              disabled={false}
              label="状态"
              onChange={setStateInput}
            />
          </label>
          <div className="history-filter-actions">
            <button type="submit" className="primary-action">
              查询
            </button>
            <button
              type="button"
              className="secondary-action"
              disabled={sessionsLoading}
              onClick={() => void loadSessions()}
            >
              刷新
            </button>
          </div>
        </form>

        {sessionsError ? (
          <p className="history-state-message" role="alert">
            {sessionsError}
          </p>
        ) : null}

        <div className="history-session-list">
          {sessionsLoading ? (
            <p className="history-state-message">加载会话...</p>
          ) : sessionList.items.length > 0 ? (
            sessionList.items.map((session) => (
              <SessionListItem
                key={session.id}
                session={session}
                isSelected={session.id === selectedSessionId}
                onSelect={setSelectedSessionId}
              />
            ))
          ) : (
            <p className="history-state-message">暂无历史会话。</p>
          )}
        </div>

        <div className="history-pagination">
          <button
            type="button"
            className="secondary-action"
            disabled={offset === 0 || sessionsLoading}
            onClick={() => setOffset(previousOffset(offset, HISTORY_PAGE_LIMIT))}
          >
            上一页
          </button>
          <span>
            {sessionList.total === 0
              ? '0 / 0'
              : `${sessionList.offset + 1}-${Math.min(
                  sessionList.offset + sessionList.items.length,
                  sessionList.total,
                )} / ${sessionList.total}`}
          </span>
          <button
            type="button"
            className="secondary-action"
            disabled={!hasNextPage || sessionsLoading}
            onClick={() => setOffset(nextOffset(offset, HISTORY_PAGE_LIMIT))}
          >
            下一页
          </button>
        </div>
      </aside>

      <section className="history-main">
        <div className="history-main-grid">
          <div className="history-detail-pane">
            <SessionDetail
              detail={detail}
              isLoading={detailLoading}
              errorMessage={detailError}
            />
          </div>
          <div className="history-task-pane">
            <VocabularyTaskPanel
              selectedSessionId={selectedSession?.id}
              tasks={tasks}
              activeTaskId={activeTaskId}
              taskDetail={taskDetail}
              isLoadingTasks={tasksLoading}
              isLoadingDetail={taskDetailLoading}
              isCreatingTask={creatingTask}
              errorMessage={taskError}
              maxWords={maxWords}
              onMaxWordsChange={setMaxWords}
              onCreateTask={handleCreateTask}
              onRefreshTasks={() => {
                if (selectedSessionId) {
                  void loadTasks(selectedSessionId)
                }
              }}
              onSelectTask={setActiveTaskId}
            />
          </div>
        </div>
      </section>
    </section>
  )
}
