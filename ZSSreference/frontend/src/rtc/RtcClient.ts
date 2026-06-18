import { RTC_ICE_SERVERS } from '../config'
import {
  WS_TYPE,
  type IceMessage,
  type WebrtcAnswerMessage,
  type WebrtcOfferMessage,
} from '../types/protocol'

export type AudioSourceKind = 'microphone' | 'tab'

export type RtcStatus = {
  connectionState: RTCPeerConnectionState | 'idle'
  iceConnectionState: RTCIceConnectionState | 'idle'
  signalingState: RTCSignalingState | 'idle'
  iceGatheringState: RTCIceGatheringState | 'idle'
}

export type RtcPrompt = {
  type: 'capture-error' | 'autoplay-blocked'
  message: string
  error?: unknown
}

export type RtcClientOptions = {
  callId: string
  userId: string
  audioSource: AudioSourceKind
  remoteAudioElement?: HTMLAudioElement | null
  mediaDevices?: Pick<MediaDevices, 'getUserMedia' | 'getDisplayMedia'>
  peerConnectionFactory?: (config: RTCConfiguration) => RTCPeerConnection
  onLocalIce?: (message: IceMessage) => void
  onStatusChange?: (status: RtcStatus) => void
  onPrompt?: (prompt: RtcPrompt) => void
}

const idleStatus: RtcStatus = {
  connectionState: 'idle',
  iceConnectionState: 'idle',
  signalingState: 'idle',
  iceGatheringState: 'idle',
}

export class RtcClient {
  private readonly callId: string
  private readonly userId: string
  private readonly audioSource: AudioSourceKind
  private readonly mediaDevices?: Pick<MediaDevices, 'getUserMedia' | 'getDisplayMedia'>
  private readonly peerConnectionFactory: (
    config: RTCConfiguration,
  ) => RTCPeerConnection
  private readonly onLocalIce?: (message: IceMessage) => void
  private readonly onStatusChange?: (status: RtcStatus) => void
  private readonly onPrompt?: (prompt: RtcPrompt) => void

  private localStream: MediaStream | null = null
  private peer: RTCPeerConnection | null = null
  private remoteAudioElement: HTMLAudioElement | null = null
  private pendingRemoteIce: RTCIceCandidateInit[] = []
  private tracksAdded = false

  constructor(options: RtcClientOptions) {
    this.callId = options.callId
    this.userId = options.userId
    this.audioSource = options.audioSource
    this.mediaDevices =
      options.mediaDevices ??
      (typeof navigator === 'undefined' ? undefined : navigator.mediaDevices)
    this.peerConnectionFactory =
      options.peerConnectionFactory ??
      ((config) => new RTCPeerConnection(config))
    this.onLocalIce = options.onLocalIce
    this.onStatusChange = options.onStatusChange
    this.onPrompt = options.onPrompt
    this.remoteAudioElement = options.remoteAudioElement ?? null
  }

  async startCapture(): Promise<MediaStream> {
    if (this.localStream) {
      return this.localStream
    }

    if (!this.mediaDevices) {
      const error = new Error('当前浏览器不支持音频采集')
      this.emitPrompt({
        type: 'capture-error',
        message: error.message,
        error,
      })
      throw error
    }

    try {
      const stream =
        this.audioSource === 'microphone'
          ? await this.mediaDevices.getUserMedia({
              audio: {
                echoCancellation: true,
                noiseSuppression: true,
                autoGainControl: true,
              },
              video: false,
            })
          : await this.mediaDevices.getDisplayMedia({ audio: true, video: true })

      this.stopVideoTracks(stream)
      this.localStream = stream
      return stream
    } catch (error) {
      const normalized = normalizeMediaError(error, this.audioSource)
      this.emitPrompt({
        type: 'capture-error',
        message: normalized.message,
        error,
      })
      throw normalized
    }
  }

  async createOffer(requestId: string): Promise<WebrtcOfferMessage> {
    const stream = await this.startCapture()
    const peer = this.ensurePeer()

    if (!this.tracksAdded) {
      for (const track of stream.getAudioTracks()) {
        peer.addTrack(track, stream)
      }
      this.tracksAdded = true
    }

    const offer = await peer.createOffer({ offerToReceiveAudio: true })
    await peer.setLocalDescription(offer)

    return {
      type: WS_TYPE.webrtc_offer,
      requestId,
      callId: this.callId,
      userId: this.userId,
      sdp: offer.sdp ?? '',
    }
  }

  async applyAnswer(message: WebrtcAnswerMessage): Promise<void> {
    const peer = this.ensurePeer()
    await peer.setRemoteDescription({ type: 'answer', sdp: message.sdp })
    await this.flushPendingRemoteIce()
  }

  async addRemoteIce(message: IceMessage): Promise<void> {
    const candidate = parseIceCandidate(message.candidate)
    if (!candidate) {
      return
    }

    if (!this.peer || !this.peer.remoteDescription) {
      this.pendingRemoteIce.push(candidate)
      return
    }

    await this.peer.addIceCandidate(candidate)
  }

  attachRemoteAudioElement(element: HTMLAudioElement | null): void {
    this.remoteAudioElement = element
  }

  close(): void {
    if (this.peer) {
      this.peer.close()
      this.peer = null
    }

    if (this.localStream) {
      for (const track of this.localStream.getTracks()) {
        track.stop()
      }
      this.localStream = null
    }

    if (this.remoteAudioElement) {
      this.remoteAudioElement.srcObject = null
    }

    this.pendingRemoteIce = []
    this.tracksAdded = false
    this.emitStatus(idleStatus)
  }

  private ensurePeer(): RTCPeerConnection {
    if (this.peer) {
      return this.peer
    }

    const peer = this.peerConnectionFactory({ iceServers: RTC_ICE_SERVERS })
    this.peer = peer

    peer.onicecandidate = (event) => {
      if (!event.candidate) {
        return
      }

      this.onLocalIce?.({
        type: WS_TYPE.ice,
        callId: this.callId,
        userId: this.userId,
        candidate: JSON.stringify({
          candidate: event.candidate.candidate,
          sdpMid: event.candidate.sdpMid,
          sdpMLineIndex: event.candidate.sdpMLineIndex,
        }),
      })
    }

    peer.onconnectionstatechange = () => this.emitCurrentStatus()
    peer.oniceconnectionstatechange = () => this.emitCurrentStatus()
    peer.onsignalingstatechange = () => this.emitCurrentStatus()
    peer.onicegatheringstatechange = () => this.emitCurrentStatus()
    peer.ontrack = (event) => this.handleRemoteTrack(event)

    this.emitCurrentStatus()
    return peer
  }

  private async flushPendingRemoteIce(): Promise<void> {
    if (!this.peer || !this.peer.remoteDescription) {
      return
    }

    const pending = this.pendingRemoteIce
    this.pendingRemoteIce = []
    for (const candidate of pending) {
      await this.peer.addIceCandidate(candidate)
    }
  }

  private handleRemoteTrack(event: RTCTrackEvent): void {
    if (!this.remoteAudioElement) {
      return
    }

    const stream = event.streams[0] ?? new MediaStream([event.track])
    this.remoteAudioElement.autoplay = true
    this.remoteAudioElement.srcObject = stream

    const playResult = this.remoteAudioElement.play()
    void playResult?.catch((error: unknown) => {
      if (isDomExceptionNamed(error, 'AbortError')) {
        return
      }
      this.emitPrompt({
        type: 'autoplay-blocked',
        message: '浏览器阻止了配音自动播放，请点击页面后重试。',
        error,
      })
    })
  }

  private emitCurrentStatus(): void {
    if (!this.peer) {
      this.emitStatus(idleStatus)
      return
    }

    this.emitStatus({
      connectionState: this.peer.connectionState,
      iceConnectionState: this.peer.iceConnectionState,
      signalingState: this.peer.signalingState,
      iceGatheringState: this.peer.iceGatheringState,
    })
  }

  private emitStatus(status: RtcStatus): void {
    this.onStatusChange?.(status)
  }

  private emitPrompt(prompt: RtcPrompt): void {
    this.onPrompt?.(prompt)
  }

  private stopVideoTracks(stream: MediaStream): void {
    for (const track of stream.getVideoTracks()) {
      track.stop()
    }
  }
}

function parseIceCandidate(candidate: string): RTCIceCandidateInit | null {
  const value = candidate.trim()
  if (!value) {
    return null
  }

  if (value.startsWith('{')) {
    try {
      const parsed = JSON.parse(value) as RTCIceCandidateInit
      return parsed.candidate ? parsed : null
    } catch {
      return null
    }
  }

  return { candidate: value }
}

function normalizeMediaError(error: unknown, source: AudioSourceKind): Error {
  if (error instanceof Error) {
    return error
  }

  const sourceLabel = source === 'microphone' ? '麦克风' : '标签页/系统音频'
  return new Error(`无法获取${sourceLabel}，请检查浏览器权限。`)
}

function isDomExceptionNamed(error: unknown, name: string): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    (error as { name?: unknown }).name === name
  )
}
