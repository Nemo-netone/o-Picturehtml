import { beforeEach, describe, expect, it, vi } from 'vitest'
import { WS_TYPE, type IceMessage } from '../types/protocol'
import { RtcClient, type RtcPrompt } from './RtcClient'

type FakeTrack = MediaStreamTrack & { stopped: boolean }

function makeTrack(kind: 'audio' | 'video' = 'audio'): FakeTrack {
  return {
    kind,
    stopped: false,
    stop() {
      this.stopped = true
    },
  } as FakeTrack
}

function makeStream(tracks: FakeTrack[]): MediaStream {
  return {
    getTracks: () => tracks,
    getAudioTracks: () => tracks.filter((track) => track.kind === 'audio'),
    getVideoTracks: () => tracks.filter((track) => track.kind === 'video'),
  } as unknown as MediaStream
}

class FakePeerConnection {
  addTrack = vi.fn()
  addIceCandidate = vi.fn(async () => {})
  close = vi.fn()
  createOffer = vi.fn(async () => ({ type: 'offer', sdp: 'offer-sdp' }))
  setLocalDescription = vi.fn(async () => {})
  setRemoteDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
    this.remoteDescription = description as RTCSessionDescription
  })

  connectionState: RTCPeerConnectionState = 'new'
  iceConnectionState: RTCIceConnectionState = 'new'
  iceGatheringState: RTCIceGatheringState = 'new'
  signalingState: RTCSignalingState = 'stable'
  remoteDescription: RTCSessionDescription | null = null

  onicecandidate: ((event: RTCPeerConnectionIceEvent) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  oniceconnectionstatechange: (() => void) | null = null
  onicegatheringstatechange: (() => void) | null = null
  onsignalingstatechange: (() => void) | null = null
  ontrack: ((event: RTCTrackEvent) => void) | null = null

  emitLocalIce(candidate: RTCIceCandidateInit): void {
    this.onicecandidate?.({ candidate } as RTCPeerConnectionIceEvent)
  }

  emitTrack(event: Partial<RTCTrackEvent>): void {
    this.ontrack?.(event as RTCTrackEvent)
  }
}

function makeClient(extra?: {
  audioSource?: 'microphone' | 'tab'
  mediaDevices?: Pick<MediaDevices, 'getUserMedia' | 'getDisplayMedia'>
  peer?: FakePeerConnection
  onLocalIce?: (message: IceMessage) => void
  onPrompt?: (prompt: RtcPrompt) => void
  remoteAudioElement?: HTMLAudioElement
}) {
  const peer = extra?.peer ?? new FakePeerConnection()
  const mediaDevices = extra?.mediaDevices ?? {
    getUserMedia: vi.fn(async () => makeStream([makeTrack('audio')])),
    getDisplayMedia: vi.fn(async () => makeStream([makeTrack('audio')])),
  }
  const client = new RtcClient({
    callId: 'call-1',
    userId: 'user-1',
    audioSource: extra?.audioSource ?? 'microphone',
    mediaDevices,
    peerConnectionFactory: () => peer as unknown as RTCPeerConnection,
    onLocalIce: extra?.onLocalIce,
    onPrompt: extra?.onPrompt,
    remoteAudioElement: extra?.remoteAudioElement,
  })

  return { client, peer }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

describe('RtcClient audio capture', () => {
  it('captures microphone audio with getUserMedia', async () => {
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client } = makeClient({ mediaDevices })

    await expect(client.startCapture()).resolves.toBe(stream)

    expect(mediaDevices.getUserMedia).toHaveBeenCalledWith({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
      },
      video: false,
    })
    expect(mediaDevices.getDisplayMedia).not.toHaveBeenCalled()
  })

  it('captures tab/system audio with getDisplayMedia and stops video tracks', async () => {
    const audio = makeTrack('audio')
    const video = makeTrack('video')
    const stream = makeStream([audio, video])
    const mediaDevices = {
      getUserMedia: vi.fn(),
      getDisplayMedia: vi.fn(async () => stream),
    }
    const { client } = makeClient({ audioSource: 'tab', mediaDevices })

    await expect(client.startCapture()).resolves.toBe(stream)

    expect(mediaDevices.getDisplayMedia).toHaveBeenCalledWith({
      audio: true,
      video: true,
    })
    expect(video.stopped).toBe(true)
    expect(audio.stopped).toBe(false)
  })

  it('surfaces capture failure through prompt and rejection', async () => {
    const error = new Error('permission denied')
    const onPrompt = vi.fn()
    const mediaDevices = {
      getUserMedia: vi.fn(async () => {
        throw error
      }),
      getDisplayMedia: vi.fn(),
    }
    const { client } = makeClient({ mediaDevices, onPrompt })

    await expect(client.startCapture()).rejects.toThrow('permission denied')

    expect(onPrompt).toHaveBeenCalledWith({
      type: 'capture-error',
      message: 'permission denied',
      error,
    })
  })

  it('stops local tracks on close', async () => {
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client } = makeClient({ mediaDevices })

    await client.startCapture()
    client.close()
    client.close()

    expect(audio.stopped).toBe(true)
  })
})

describe('RtcClient offer/answer', () => {
  it('adds audio tracks, creates an offer, and returns protocol message', async () => {
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client, peer } = makeClient({ mediaDevices })

    const offer = await client.createOffer('offer-1')

    expect(peer.addTrack).toHaveBeenCalledWith(audio, stream)
    expect(peer.createOffer).toHaveBeenCalledWith({ offerToReceiveAudio: true })
    expect(peer.setLocalDescription).toHaveBeenCalledWith({
      type: 'offer',
      sdp: 'offer-sdp',
    })
    expect(offer).toEqual({
      type: WS_TYPE.webrtc_offer,
      requestId: 'offer-1',
      callId: 'call-1',
      userId: 'user-1',
      sdp: 'offer-sdp',
    })
  })

  it('applies answer and flushes queued remote ICE', async () => {
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client, peer } = makeClient({ mediaDevices })

    await client.createOffer('offer-1')
    await client.addRemoteIce({
      type: WS_TYPE.ice,
      callId: 'call-1',
      userId: 'user-1',
      candidate: JSON.stringify({
        candidate: 'candidate:remote',
        sdpMid: '0',
        sdpMLineIndex: 0,
      }),
    })
    expect(peer.addIceCandidate).not.toHaveBeenCalled()

    await client.applyAnswer({
      type: WS_TYPE.webrtc_answer,
      requestId: 'offer-1',
      callId: 'call-1',
      userId: 'user-1',
      sdp: 'answer-sdp',
    })

    expect(peer.setRemoteDescription).toHaveBeenCalledWith({
      type: 'answer',
      sdp: 'answer-sdp',
    })
    expect(peer.addIceCandidate).toHaveBeenCalledWith({
      candidate: 'candidate:remote',
      sdpMid: '0',
      sdpMLineIndex: 0,
    })
  })
})

describe('RtcClient ICE', () => {
  it('emits local ICE as a JSON-stringified protocol candidate', async () => {
    const onLocalIce = vi.fn()
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client, peer } = makeClient({ mediaDevices, onLocalIce })

    await client.createOffer('offer-1')
    peer.emitLocalIce({
      candidate: 'candidate:local',
      sdpMid: 'audio',
      sdpMLineIndex: 0,
    })

    expect(onLocalIce).toHaveBeenCalledWith({
      type: WS_TYPE.ice,
      callId: 'call-1',
      userId: 'user-1',
      candidate: JSON.stringify({
        candidate: 'candidate:local',
        sdpMid: 'audio',
        sdpMLineIndex: 0,
      }),
    })
  })

  it('accepts raw remote ICE candidates after the answer is applied', async () => {
    const audio = makeTrack('audio')
    const stream = makeStream([audio])
    const mediaDevices = {
      getUserMedia: vi.fn(async () => stream),
      getDisplayMedia: vi.fn(),
    }
    const { client, peer } = makeClient({ mediaDevices })

    await client.createOffer('offer-1')
    await client.applyAnswer({
      type: WS_TYPE.webrtc_answer,
      requestId: 'offer-1',
      callId: 'call-1',
      userId: 'user-1',
      sdp: 'answer-sdp',
    })
    await client.addRemoteIce({
      type: WS_TYPE.ice,
      callId: 'call-1',
      userId: 'user-1',
      candidate: 'candidate:raw',
    })

    expect(peer.addIceCandidate).toHaveBeenLastCalledWith({
      candidate: 'candidate:raw',
    })
  })
})

describe('RtcClient remote audio', () => {
  it('attaches the remote stream to audio element and calls play', async () => {
    const play = vi.fn(async () => {})
    const audioElement = { play, autoplay: false, srcObject: null } as unknown as
      | HTMLAudioElement
      | undefined
    const remoteStream = makeStream([makeTrack('audio')])
    const { client, peer } = makeClient({ remoteAudioElement: audioElement })

    await client.createOffer('offer-1')
    peer.emitTrack({ streams: [remoteStream] })

    expect(audioElement?.autoplay).toBe(true)
    expect(audioElement?.srcObject).toBe(remoteStream)
    expect(play).toHaveBeenCalled()
  })

  it('ignores AbortError autoplay failures', async () => {
    const onPrompt = vi.fn()
    const play = vi.fn(async () => {
      throw { name: 'AbortError' }
    })
    const audioElement = { play, autoplay: false, srcObject: null } as unknown as
      | HTMLAudioElement
      | undefined
    const { client, peer } = makeClient({ onPrompt, remoteAudioElement: audioElement })

    await client.createOffer('offer-1')
    peer.emitTrack({ streams: [makeStream([makeTrack('audio')])] })
    await Promise.resolve()

    expect(onPrompt).not.toHaveBeenCalled()
  })

  it('prompts when autoplay is blocked', async () => {
    const onPrompt = vi.fn()
    const error = { name: 'NotAllowedError' }
    const play = vi.fn(async () => {
      throw error
    })
    const audioElement = { play, autoplay: false, srcObject: null } as unknown as
      | HTMLAudioElement
      | undefined
    const { client, peer } = makeClient({ onPrompt, remoteAudioElement: audioElement })

    await client.createOffer('offer-1')
    peer.emitTrack({ streams: [makeStream([makeTrack('audio')])] })
    await Promise.resolve()

    expect(onPrompt).toHaveBeenCalledWith({
      type: 'autoplay-blocked',
      message: '浏览器阻止了配音自动播放，请点击页面后重试。',
      error,
    })
  })
})
