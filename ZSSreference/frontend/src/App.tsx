import { useMemo, useState } from 'react'

import './App.css'
import { ControlBar } from './components/ControlBar'
import { HistoryPage } from './components/HistoryPage'
import { NodeStatusPage } from './components/NodeStatusPage'
import { SubtitlePiPButton } from './components/SubtitlePiPButton'
import type { SubtitlePiPControls } from './components/SubtitlePiPPanel'
import { SubtitlePanel } from './components/SubtitlePanel'
import { TopBar } from './components/TopBar'
import { appConfig } from './config'
import { useSession, type SessionStatus } from './session/useSession'
import type { Strategy } from './types/protocol'

function toTopBarStatus(status: SessionStatus) {
  if (status === 'starting') {
    return 'connecting'
  }
  if (status === 'running') {
    return 'connected'
  }
  if (status === 'stopped' || status === 'error') {
    return 'disconnected'
  }
  return 'idle'
}

function toPiPSessionStatus(status: SessionStatus) {
  return toTopBarStatus(status)
}

type AppView = 'live' | 'history' | 'nodes'

function App() {
  const [activeView, setActiveView] = useState<AppView>('live')
  const {
    attachRemoteAudioElement,
    audioSource,
    callId,
    dubbing,
    errorMessage,
    isRunning,
    isStarting,
    isStopping,
    isStrategyPending,
    audioLevel,
    promptMessage,
    setAudioSource,
    setDubbing,
    setSourceLanguage,
    setStrategy,
    setTargetLanguage,
    sourceLanguage,
    start,
    status,
    stop,
    strategy,
    targetLanguage,
  } = useSession()

  const pipControls = useMemo<SubtitlePiPControls>(
    () => ({
      audioSource,
      strategy,
      dubbing,
      isStarting,
      isRunning,
      isStopping,
      isStrategyPending,
      errorMessage,
      promptMessage,
      onAudioSourceChange: setAudioSource,
      onStrategyChange: (nextStrategy: Strategy) => void setStrategy(nextStrategy),
      onDubbingChange: setDubbing,
      onStart: () => void start(),
      onStop: stop,
    }),
    [
      audioSource,
      strategy,
      dubbing,
      isStarting,
      isRunning,
      isStopping,
      isStrategyPending,
      errorMessage,
      promptMessage,
      setAudioSource,
      setStrategy,
      setDubbing,
      start,
      stop,
    ],
  )

  return (
    <main className="app-shell">
      <TopBar
        status={toTopBarStatus(status)}
        latencyLabel="--"
        sessionId={callId}
        serviceUrl={
          activeView === 'live'
            ? appConfig.wsUrl
            : appConfig.apiHttpUrl || '同源 /api'
        }
        actions={
          <div className="top-actions-cluster">
            <div className="view-tabs" role="tablist" aria-label="页面切换">
              <button
                type="button"
                role="tab"
                aria-selected={activeView === 'live'}
                className="view-tab"
                onClick={() => setActiveView('live')}
              >
                实时同传
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeView === 'history'}
                className="view-tab"
                onClick={() => setActiveView('history')}
              >
                历史会话
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeView === 'nodes'}
                className="view-tab"
                onClick={() => setActiveView('nodes')}
              >
                节点状态
              </button>
            </div>
            <SubtitlePiPButton
              sessionStatus={toPiPSessionStatus(status)}
              controls={pipControls}
            />
          </div>
        }
      />
      {activeView === 'live' ? (
        <div className="dashboard-layout">
          <aside className="dashboard-sidebar">
            <ControlBar
              audioSource={audioSource}
              strategy={strategy}
              dubbing={dubbing}
              sourceLanguage={sourceLanguage}
              targetLanguage={targetLanguage}
              isStarting={isStarting}
              isRunning={isRunning}
              isStopping={isStopping}
              isStrategyPending={isStrategyPending}
              audioLevel={audioLevel}
              errorMessage={errorMessage}
              promptMessage={promptMessage}
              onAudioSourceChange={setAudioSource}
              onSourceLanguageChange={setSourceLanguage}
              onTargetLanguageChange={setTargetLanguage}
              onStrategyChange={(nextStrategy) => void setStrategy(nextStrategy)}
              onDubbingChange={setDubbing}
              onStart={() => void start()}
              onStop={stop}
            />
          </aside>
          <section className="dashboard-main">
            <SubtitlePanel />
          </section>
        </div>
      ) : activeView === 'history' ? (
        <HistoryPage />
      ) : (
        <NodeStatusPage />
      )}
      <audio
        ref={attachRemoteAudioElement}
        className="session-audio"
        autoPlay
      />
    </main>
  )
}

export default App
