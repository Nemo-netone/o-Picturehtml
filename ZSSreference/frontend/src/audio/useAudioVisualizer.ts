import { useCallback, useEffect, useRef, useState } from 'react'

type AudioContextConstructor = new () => AudioContext

type AudioGlobal = Pick<Window, 'cancelAnimationFrame' | 'requestAnimationFrame'> & {
  AudioContext?: AudioContextConstructor
  webkitAudioContext?: AudioContextConstructor
}

type AnalyzerState = {
  context: AudioContext
  source: MediaStreamAudioSourceNode
  analyser: AnalyserNode
  data: Uint8Array
  frame: number
}

export type AudioLevelAnalyzer = {
  start: (stream: MediaStream, onLevel: (level: number) => void) => void
  stop: () => void
}

export type UseAudioVisualizerResult = {
  audioLevel: number
  start: (stream: MediaStream) => void
  stop: () => void
}

function audioContextCtor(target: AudioGlobal): AudioContextConstructor | undefined {
  return target.AudioContext ?? target.webkitAudioContext
}

function safeDisconnect(node: { disconnect: () => void }): void {
  try {
    node.disconnect()
  } catch {
    // Disconnect can throw if the node is already detached; stop remains idempotent.
  }
}

export function createAudioLevelAnalyzer(
  target: AudioGlobal = window,
): AudioLevelAnalyzer {
  let state: AnalyzerState | null = null

  const stop = () => {
    if (!state) {
      return
    }

    target.cancelAnimationFrame(state.frame)
    safeDisconnect(state.source)
    safeDisconnect(state.analyser)
    if (state.context.state !== 'closed') {
      void state.context.close().catch(() => undefined)
    }
    state = null
  }

  const start = (stream: MediaStream, onLevel: (level: number) => void) => {
    stop()

    const AudioContextCtor = audioContextCtor(target)
    if (!AudioContextCtor) {
      onLevel(0)
      return
    }

    const context = new AudioContextCtor()
    const source = context.createMediaStreamSource(stream)
    const analyser = context.createAnalyser()
    analyser.fftSize = 256
    source.connect(analyser)

    const data = new Uint8Array(analyser.frequencyBinCount)
    const sample = () => {
      if (!state) {
        return
      }

      analyser.getByteFrequencyData(data)
      const average =
        data.length === 0
          ? 0
          : data.reduce((sum, value) => sum + value, 0) / data.length
      onLevel(Math.min(1, average / 160))

      if (state) {
        state.frame = target.requestAnimationFrame(sample)
      }
    }

    state = {
      context,
      source,
      analyser,
      data,
      frame: target.requestAnimationFrame(sample),
    }
  }

  return { start, stop }
}

export function useAudioVisualizer(): UseAudioVisualizerResult {
  const [audioLevel, setAudioLevel] = useState(0)
  const analyzerRef = useRef<AudioLevelAnalyzer | null>(null)

  const stop = useCallback(() => {
    analyzerRef.current?.stop()
    setAudioLevel(0)
  }, [])

  const start = useCallback(
    (stream: MediaStream) => {
      if (!analyzerRef.current) {
        analyzerRef.current = createAudioLevelAnalyzer()
      }
      analyzerRef.current.start(stream, setAudioLevel)
    },
    [],
  )

  useEffect(() => stop, [stop])

  return { audioLevel, start, stop }
}
