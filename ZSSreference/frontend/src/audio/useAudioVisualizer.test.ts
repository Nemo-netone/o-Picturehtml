import { describe, expect, it, vi } from 'vitest'

import { createAudioLevelAnalyzer } from './useAudioVisualizer'

type FakeAudioNode = {
  disconnect: ReturnType<typeof vi.fn>
}

type FakeAnalyser = FakeAudioNode & {
  fftSize: number
  frequencyBinCount: number
  getByteFrequencyData: (data: Uint8Array) => void
}

function makeStream(): MediaStream {
  return {} as MediaStream
}

function makeAudioTarget() {
  const source = {
    connect: vi.fn(),
    disconnect: vi.fn(),
  }
  const analyser: FakeAnalyser = {
    fftSize: 0,
    frequencyBinCount: 4,
    disconnect: vi.fn(),
    getByteFrequencyData: vi.fn((data: Uint8Array) => {
      data.set([80, 120, 160, 200])
    }),
  }
  const context = {
    state: 'running' as AudioContextState,
    close: vi.fn(async () => {
      context.state = 'closed'
    }),
    createAnalyser: vi.fn(() => analyser as unknown as AnalyserNode),
    createMediaStreamSource: vi.fn(
      () => source as unknown as MediaStreamAudioSourceNode,
    ),
  }
  const target = {
    AudioContext: vi.fn(() => context as unknown as AudioContext),
    cancelAnimationFrame: vi.fn(),
    requestAnimationFrame: vi.fn((callback: FrameRequestCallback) => {
      void callback
      return 7
    }),
  }

  return { analyser, context, source, target }
}

describe('createAudioLevelAnalyzer', () => {
  it('starts analysis from an existing media stream', () => {
    const { analyser, context, source, target } = makeAudioTarget()
    const onLevel = vi.fn()
    const analyzer = createAudioLevelAnalyzer(target as never)
    const stream = makeStream()

    analyzer.start(stream, onLevel)
    const sample = target.requestAnimationFrame.mock.calls[0]?.[0]
    sample?.(0)

    expect(context.createMediaStreamSource).toHaveBeenCalledWith(stream)
    expect(source.connect).toHaveBeenCalledWith(analyser)
    expect(analyser.fftSize).toBe(256)
    expect(onLevel).toHaveBeenCalledWith(0.875)
    expect(target.requestAnimationFrame).toHaveBeenCalledTimes(2)
  })

  it('does not request a new media stream', () => {
    const { target } = makeAudioTarget()
    const getUserMedia = vi.fn()
    const previousNavigator = globalThis.navigator
    Object.defineProperty(globalThis, 'navigator', {
      configurable: true,
      value: { mediaDevices: { getUserMedia } },
    })

    createAudioLevelAnalyzer(target as never).start(makeStream(), vi.fn())

    expect(getUserMedia).not.toHaveBeenCalled()
    Object.defineProperty(globalThis, 'navigator', {
      configurable: true,
      value: previousNavigator,
    })
  })

  it('cleans up animation frame, nodes, and audio context on stop', () => {
    const { analyser, context, source, target } = makeAudioTarget()
    const analyzer = createAudioLevelAnalyzer(target as never)

    analyzer.start(makeStream(), vi.fn())
    analyzer.stop()
    analyzer.stop()

    expect(target.cancelAnimationFrame).toHaveBeenCalledTimes(1)
    expect(target.cancelAnimationFrame).toHaveBeenCalledWith(7)
    expect(source.disconnect).toHaveBeenCalledTimes(1)
    expect(analyser.disconnect).toHaveBeenCalledTimes(1)
    expect(context.close).toHaveBeenCalledTimes(1)
  })

  it('replaces the previous analyzer when started repeatedly', () => {
    const first = makeAudioTarget()
    const second = makeAudioTarget()
    const contexts = [
      first.context as unknown as AudioContext,
      second.context as unknown as AudioContext,
    ]
    const target = {
      AudioContext: vi.fn(() => contexts.shift()!),
      cancelAnimationFrame: vi.fn(),
      requestAnimationFrame: vi.fn((callback: FrameRequestCallback) => {
        void callback
        return 11
      }),
    }
    const analyzer = createAudioLevelAnalyzer(target as never)

    analyzer.start(makeStream(), vi.fn())
    analyzer.start(makeStream(), vi.fn())

    expect(first.source.disconnect).toHaveBeenCalledTimes(1)
    expect(first.analyser.disconnect).toHaveBeenCalledTimes(1)
    expect(first.context.close).toHaveBeenCalledTimes(1)
    expect(second.source.disconnect).not.toHaveBeenCalled()
  })
})
