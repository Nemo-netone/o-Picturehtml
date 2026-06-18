import { useCallback, useEffect, useRef, useState } from 'react'

import {
  ASR_ENGINE_BY_SOURCE_LANGUAGE,
  DEFAULT_TTS_VOICE_BY_TARGET_LANGUAGE,
} from './languageOptions'
import { appConfig } from '../config'
import { useAudioVisualizer } from '../audio/useAudioVisualizer'
import {
  type AudioSourceKind,
  RtcClient,
  type RtcStatus,
} from '../rtc/RtcClient'
import { useSubtitleStore } from '../state/subtitles'
import {
  WS_TYPE,
  type DubbingFlag,
  type ErrorMessage,
  type SetStrategyAckMessage,
  type SourceLanguage,
  type Strategy,
  type TargetLanguage,
} from '../types/protocol'
import { type WsConnectionStatus, WsClient } from '../ws/WsClient'

const SESSION_WAIT_TIMEOUT_MS = 8000

export type SessionAudioSource = AudioSourceKind

export type SessionStatus =
  | 'idle'
  | 'starting'
  | 'running'
  | 'stopping'
  | 'stopped'
  | 'error'

export type UseSessionResult = {
  audioSource: SessionAudioSource
  strategy: Strategy
  dubbing: boolean
  sourceLanguage: SourceLanguage
  targetLanguage: TargetLanguage
  status: SessionStatus
  wsStatus: WsConnectionStatus
  rtcStatus: RtcStatus
  callId: string
  errorMessage?: string
  promptMessage?: string
  pendingStrategy?: Strategy
  audioLevel: number
  isStarting: boolean
  isRunning: boolean
  isStopping: boolean
  isStrategyPending: boolean
  setAudioSource: (source: SessionAudioSource) => void
  setSourceLanguage: (language: SourceLanguage) => void
  setTargetLanguage: (language: TargetLanguage) => void
  setDubbing: (enabled: boolean) => void
  setStrategy: (strategy: Strategy) => Promise<void>
  start: () => Promise<void>
  stop: () => void
  attachRemoteAudioElement: (element: HTMLAudioElement | null) => void
}

type Unsubscribe = () => void

const idleRtcStatus: RtcStatus = {
  connectionState: 'idle',
  iceConnectionState: 'idle',
  signalingState: 'idle',
  iceGatheringState: 'idle',
}

function toDubbingFlag(enabled: boolean): DubbingFlag {
  return enabled ? '1' : '0'
}

function createCallId(): string {
  return `call-${crypto.randomUUID?.() ?? Math.floor(performance.now())}`
}

function messageFromError(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message
  }
  return '会话操作失败，请检查服务状态后重试。'
}

export function useSession(): UseSessionResult {
  const [audioSource, setAudioSourceState] =
    useState<SessionAudioSource>('microphone')
  const [strategy, setConfirmedStrategy] = useState<Strategy>('hybrid')
  const [dubbing, setDubbingState] = useState(true)
  const [sourceLanguage, setSourceLanguageState] =
    useState<SourceLanguage>('en')
  const [targetLanguage, setTargetLanguageState] =
    useState<TargetLanguage>('zh')
  const [status, setStatus] = useState<SessionStatus>('idle')
  const [wsStatus, setWsStatus] = useState<WsConnectionStatus>('idle')
  const [rtcStatus, setRtcStatus] = useState<RtcStatus>(idleRtcStatus)
  const [callId, setCallId] = useState(appConfig.clientId)
  const [errorMessage, setErrorMessage] = useState<string>()
  const [promptMessage, setPromptMessage] = useState<string>()
  const [pendingStrategy, setPendingStrategy] = useState<Strategy>()
  const {
    audioLevel,
    start: startAudioVisualizer,
    stop: stopAudioVisualizer,
  } = useAudioVisualizer()

  const wsRef = useRef<WsClient | null>(null)
  const rtcRef = useRef<RtcClient | null>(null)
  const audioElementRef = useRef<HTMLAudioElement | null>(null)
  const unsubscribersRef = useRef<Unsubscribe[]>([])
  const stopRequestedRef = useRef(false)

  const cleanupSubscriptions = useCallback(() => {
    for (const unsubscribe of unsubscribersRef.current) {
      unsubscribe()
    }
    unsubscribersRef.current = []
  }, [])

  const closeClients = useCallback(() => {
    stopAudioVisualizer()
    cleanupSubscriptions()
    rtcRef.current?.close()
    wsRef.current?.close()
    rtcRef.current = null
    wsRef.current = null
    setPendingStrategy(undefined)
    setWsStatus('closed')
    setRtcStatus(idleRtcStatus)
  }, [cleanupSubscriptions, stopAudioVisualizer])

  const stop = useCallback(() => {
    stopRequestedRef.current = true
    setStatus((current) =>
      current === 'idle' || current === 'stopped' ? current : 'stopping',
    )
    closeClients()
    setStatus('stopped')
  }, [closeClients])

  useEffect(() => closeClients, [closeClients])

  const attachRemoteAudioElement = useCallback(
    (element: HTMLAudioElement | null) => {
      audioElementRef.current = element
      rtcRef.current?.attachRemoteAudioElement(element)
    },
    [],
  )

  const waitForHelloAck = useCallback((ws: WsClient): Promise<void> => {
    return new Promise((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        unsubscribe()
        reject(new Error('等待 client_hello_ack 超时'))
      }, SESSION_WAIT_TIMEOUT_MS)

      const unsubscribe = ws.on(WS_TYPE.client_hello_ack, () => {
        window.clearTimeout(timeout)
        unsubscribe()
        resolve()
      })
    })
  }, [])

  const start = useCallback(async () => {
    if (status === 'starting' || status === 'running') {
      return
    }

    stopRequestedRef.current = false
    setStatus('starting')
    setErrorMessage(undefined)
    setPromptMessage(undefined)
    setPendingStrategy(undefined)

    if (sourceLanguage === targetLanguage) {
      setErrorMessage('输入语言和输出语言不能相同，请调整后再开始。')
      setStatus('idle')
      return
    }

    const nextCallId = createCallId()
    setCallId(nextCallId)

    const rtc = new RtcClient({
      callId: nextCallId,
      userId: appConfig.clientId,
      audioSource,
      remoteAudioElement: audioElementRef.current,
      onLocalIce: (message) => {
        wsRef.current?.send(message)
      },
      onStatusChange: setRtcStatus,
      onPrompt: (prompt) => {
        setPromptMessage(prompt.message)
      },
    })
    rtcRef.current = rtc

    try {
      const stream = await rtc.startCapture()
      startAudioVisualizer(stream)

      if (stopRequestedRef.current) {
        closeClients()
        setStatus('stopped')
        return
      }

      const ws = new WsClient({
        url: appConfig.wsUrl,
        tenantId: appConfig.tenantId,
        clientId: appConfig.clientId,
        handshake: {
          translateStrategy: strategy,
          dubbing: toDubbingFlag(dubbing),
          responseMode: 'compact',
          sourceLanguage,
          targetLanguage,
          asrEngineType: ASR_ENGINE_BY_SOURCE_LANGUAGE[sourceLanguage],
          ttsVoiceType: DEFAULT_TTS_VOICE_BY_TARGET_LANGUAGE[targetLanguage],
        },
      })
      wsRef.current = ws

      unsubscribersRef.current = [
        ws.onStatusChange(setWsStatus),
        ws.on(WS_TYPE.asr_final, (message) => {
          useSubtitleStore.getState().dispatch(message)
        }),
        ws.on(WS_TYPE.tmt_final, (message) => {
          useSubtitleStore.getState().dispatch(message)
        }),
        ws.on(WS_TYPE.llm_tmt_final, (message) => {
          useSubtitleStore.getState().dispatch(message)
        }),
        ws.on(WS_TYPE.asr_result, (message) => {
          useSubtitleStore.getState().dispatch(message)
        }),
        ws.on(WS_TYPE.translation_result, (message) => {
          useSubtitleStore.getState().dispatch(message)
        }),
        ws.on(WS_TYPE.webrtc_answer, (message) => {
          void rtc.applyAnswer(message).catch((error: unknown) => {
            setErrorMessage(messageFromError(error))
          })
        }),
        ws.on(WS_TYPE.ice, (message) => {
          void rtc.addRemoteIce(message).catch((error: unknown) => {
            setErrorMessage(messageFromError(error))
          })
        }),
        ws.on(WS_TYPE.error, (message: ErrorMessage) => {
          setErrorMessage(message.error)
        }),
      ]

      ws.connect()
      await waitForHelloAck(ws)

      if (stopRequestedRef.current) {
        closeClients()
        setStatus('stopped')
        return
      }

      const offer = await rtc.createOffer(ws.nextRequestId('offer'))
      ws.send(offer)
      setStatus('running')
    } catch (error) {
      closeClients()
      setErrorMessage(messageFromError(error))
      setStatus('error')
      setWsStatus('closed')
      setRtcStatus(idleRtcStatus)
    }
  }, [
    audioSource,
    closeClients,
    dubbing,
    sourceLanguage,
    startAudioVisualizer,
    status,
    strategy,
    targetLanguage,
    waitForHelloAck,
  ])

  const setStrategy = useCallback(
    async (nextStrategy: Strategy) => {
      if (nextStrategy === strategy || pendingStrategy) {
        return
      }

      const ws = wsRef.current
      if (status !== 'running' || !ws) {
        setConfirmedStrategy(nextStrategy)
        return
      }

      const requestId = ws.nextRequestId('strategy')
      setPendingStrategy(nextStrategy)
      setErrorMessage(undefined)

      try {
        await new Promise<void>((resolve, reject) => {
          const timeout = window.setTimeout(() => {
            unsubscribe()
            reject(new Error('等待 set_strategy_ack 超时'))
          }, SESSION_WAIT_TIMEOUT_MS)

          const unsubscribe = ws.on(
            WS_TYPE.set_strategy_ack,
            (message: SetStrategyAckMessage) => {
              if (message.requestId !== requestId) {
                return
              }
              window.clearTimeout(timeout)
              unsubscribe()
              setConfirmedStrategy(message.metadata.translateStrategy)
              resolve()
            },
          )

          ws.send({
            type: WS_TYPE.set_strategy,
            requestId,
            metadata: { translateStrategy: nextStrategy },
          })
        })
      } catch (error) {
        setErrorMessage(messageFromError(error))
      } finally {
        setPendingStrategy(undefined)
      }
    },
    [pendingStrategy, status, strategy],
  )

  const setAudioSource = useCallback((source: SessionAudioSource) => {
    setAudioSourceState(source)
  }, [])

  const setSourceLanguage = useCallback((language: SourceLanguage) => {
    setSourceLanguageState(language)
  }, [])

  const setTargetLanguage = useCallback((language: TargetLanguage) => {
    setTargetLanguageState(language)
  }, [])

  const setDubbing = useCallback((enabled: boolean) => {
    setDubbingState(enabled)
  }, [])

  return {
    audioSource,
    strategy,
    dubbing,
    sourceLanguage,
    targetLanguage,
    status,
    wsStatus,
    rtcStatus,
    callId,
    errorMessage,
    promptMessage,
    pendingStrategy,
    audioLevel,
    isStarting: status === 'starting',
    isRunning: status === 'running',
    isStopping: status === 'stopping',
    isStrategyPending: pendingStrategy !== undefined,
    setAudioSource,
    setSourceLanguage,
    setTargetLanguage,
    setDubbing,
    setStrategy,
    start,
    stop,
    attachRemoteAudioElement,
  }
}
