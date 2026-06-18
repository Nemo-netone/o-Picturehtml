import React from "react";
import { createRoot } from "react-dom/client";
import { Activity, Cable, Mic, Phone, Play, RefreshCw, RotateCcw, Square, Volume2, Waves } from "lucide-react";
import "./styles.css";

type WSMessage = {
  type: string;
  requestId?: string;
  connectionId?: string;
  tenantId?: string;
  extension?: string;
  clientId?: string;
  responseMode?: string;
  callId?: string;
  userId?: string;
  sdp?: string;
  candidate?: string;
  text?: string;
  utteranceId?: string;
  sourceText?: string;
  engine?: string;
  revised?: boolean;
  audio?: string;
  format?: string;
  sampleRate?: number;
  sequence?: number;
  isLast?: boolean;
  isFinal?: boolean;
  confidence?: number;
  voice?: string;
  language?: string;
  metadata?: Record<string, string>;
  error?: string;
};

type IcePayload = {
  candidate: string;
  sdpMid?: string | null;
  sdpMLineIndex?: number | null;
};

type LogEntry = {
  id: number;
  time: string;
  title: string;
  payload?: unknown;
};

type DisplayLine = {
  key: string;
  value: string;
};

type TranslationBucket = "tmt" | "llm";

type AudioSource = {
  mode: AudioMode;
  context: AudioContext;
  stream: MediaStream;
  inputStreams?: MediaStream[];
  sourceNodes?: MediaStreamAudioSourceNode[];
  oscillator?: OscillatorNode;
  gain?: GainNode;
  destination?: MediaStreamAudioDestinationNode;
};

type MeterTarget = {
  context: AudioContext;
  source: MediaStreamAudioSourceNode;
  analyser: AnalyserNode;
  samples: Uint8Array<ArrayBuffer>;
};

type AudioMode = "tone" | "mic" | "system" | "mixed";

type ExtendedDisplayMediaOptions = DisplayMediaStreamOptions & {
  systemAudio?: "include" | "exclude";
  surfaceSwitching?: "include" | "exclude";
  selfBrowserSurface?: "include" | "exclude";
};

type AudioProcessingState = {
  echoCancellation: boolean;
  noiseSuppression: boolean;
  autoGainControl: boolean;
};

type FormState = {
  wsUrl: string;
  tenantId: string;
  clientId: string;
  responseMode: string;
  translateStrategy: string;
  dubbing: boolean;
  callId: string;
  userId: string;
};

type TextFormKey = Exclude<keyof FormState, "dubbing">;

const defaultForm: FormState = {
  wsUrl: "ws://127.0.0.1:8080/ws?token=tenant-a:react-probe",
  tenantId: "tenant-a",
  clientId: "react-probe",
  responseMode: "debug",
  translateStrategy: "hybrid",
  dubbing: false,
  callId: `react-call-${Date.now()}`,
  userId: "react-user-001",
};

const defaultAudioProcessing: AudioProcessingState = {
  echoCancellation: true,
  noiseSuppression: true,
  autoGainControl: true,
};

let logSequence = 0;

// requestID 生成 WebSocket 请求 ID，方便在控制台串联一次请求。
function requestID(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

// formatPayload 把日志 payload 转成页面可读文本。
function formatPayload(payload: unknown): string {
  if (payload === undefined) {
    return "";
  }
  return JSON.stringify(payload, null, 2);
}

function isTranslationMessage(type: string): boolean {
  return type === "translation_result" || type === "tmt_final" || type === "tmt_result" || type === "llm_tmt_final";
}

function translationBucket(message: WSMessage): TranslationBucket {
  if (message.type === "tmt_final" || message.type === "tmt_result") {
    return "tmt";
  }
  if (message.type === "llm_tmt_final") {
    return "llm";
  }
  const engine = (message.engine || "").toLowerCase();
  if (engine === "tmt") {
    return "tmt";
  }
  return "llm";
}

function formatTranslationLine(message: WSMessage): string {
  const source = message.sourceText ? `\nEN: ${message.sourceText}` : "";
  const revised = message.revised ? " revised" : "";
  return `[${new Date().toLocaleTimeString()}] ${message.engine || message.type} ${message.isFinal ? "final" : "partial"}${revised}\n${message.text}${source}`;
}

// isMediaAbortError 判断媒体播放是否被浏览器正常中断。
function isMediaAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

// formatMediaPlayError 把媒体播放失败信息整理成稳定的日志文本。
function formatMediaPlayError(error: unknown): string {
  if (error instanceof Error) {
    return `${error.name}: ${error.message}`;
  }
  return String(error);
}

// redactWSMessageForLog 保留日志入口，后续若有敏感字段可统一在这里处理。
function redactWSMessageForLog(message: WSMessage): WSMessage {
  return JSON.parse(JSON.stringify(message)) as WSMessage;
}

// parseICE 解析 api-server 转发的 JSON candidate 或纯 candidate 字符串。
function parseICE(value: string): IcePayload {
  const text = value.trim();
  if (text.startsWith("{")) {
    return JSON.parse(text) as IcePayload;
  }
  return { candidate: text };
}

// audioMimeType 根据 TTS 返回格式选择浏览器可播放 MIME。
function audioMimeType(format?: string): string {
  switch ((format || "").toLowerCase()) {
    case "mp3":
      return "audio/mpeg";
    case "wav":
      return "audio/wav";
    case "pcm":
      return "application/octet-stream";
    default:
      return "audio/wav";
  }
}

// base64ToBlob 把后端 WebSocket 返回的 base64 音频转成 Blob。
function base64ToBlob(base64: string, type: string): Blob {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new Blob([bytes], { type });
}

// summarizeCandidate 从 candidate 字符串中提取关键信息，控制台排查时更容易看。
function summarizeCandidate(candidate: string): Record<string, string> {
  const fields = candidate.trim().replace(/^candidate:/, "").split(/\s+/);
  const summary: Record<string, string> = {
    protocol: fields[2] || "",
    address: fields[4] || "",
    port: fields[5] || "",
    raw: candidate.slice(0, 160),
  };
  fields.forEach((field, index) => {
    if (field === "typ" && fields[index + 1]) {
      summary.type = fields[index + 1];
    }
    if (field === "tcptype" && fields[index + 1]) {
      summary.tcpType = fields[index + 1];
    }
  });
  return summary;
}

// createAudioContext 创建兼容不同浏览器前缀的 AudioContext。
function createAudioContext(): AudioContext {
  const AudioCtor = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
  return new AudioCtor();
}

// createToneSource 创建浏览器生成的测试音频流；加入 PeerConnection 后浏览器会持续发送 RTP。
function createToneSource(): AudioSource {
  const context = createAudioContext();
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  const destination = context.createMediaStreamDestination();
  oscillator.type = "sine";
  oscillator.frequency.value = 440;
  gain.gain.value = 0.04;
  oscillator.connect(gain);
  gain.connect(destination);
  oscillator.start();
  return { mode: "tone", context, oscillator, gain, destination, stream: destination.stream };
}

// createMicSource 请求麦克风输入流，并开启浏览器 WebRTC APM 音频处理能力。
async function createMicSource(deviceId: string, processing: AudioProcessingState): Promise<AudioSource> {
  const audio: MediaTrackConstraints = {
    echoCancellation: processing.echoCancellation,
    noiseSuppression: processing.noiseSuppression,
    autoGainControl: processing.autoGainControl,
  };
  if (deviceId) {
    audio.deviceId = { exact: deviceId };
  }
  const stream = await navigator.mediaDevices.getUserMedia({
    audio,
    video: false,
  });
  return { mode: "mic", context: createAudioContext(), stream };
}

// createSystemSource 通过屏幕共享授权采集系统/标签页音频。浏览器通常要求同时请求 video。
async function createSystemSource(): Promise<AudioSource> {
  if (!navigator.mediaDevices?.getDisplayMedia) {
    throw new Error("浏览器不支持系统声音采集");
  }
  const stream = await navigator.mediaDevices.getDisplayMedia({
    video: true,
    audio: {
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false,
    },
    systemAudio: "include",
    surfaceSwitching: "include",
    selfBrowserSurface: "exclude",
  } as ExtendedDisplayMediaOptions);
  if (stream.getAudioTracks().length === 0) {
    stream.getTracks().forEach((track) => track.stop());
    throw new Error("没有获得系统声音轨道，请在浏览器共享弹窗中勾选共享音频");
  }
  return { mode: "system", context: createAudioContext(), stream, inputStreams: [stream] };
}

// createMixedSource 混合麦克风和系统声音，输出单条音频 track 给 WebRTC。
async function createMixedSource(deviceId: string, processing: AudioProcessingState): Promise<AudioSource> {
  const mic = await createMicSource(deviceId, processing);
  let system: AudioSource | null = null;
  try {
    system = await createSystemSource();
    const context = createAudioContext();
    const destination = context.createMediaStreamDestination();
    const micNode = context.createMediaStreamSource(mic.stream);
    const systemNode = context.createMediaStreamSource(system.stream);
    micNode.connect(destination);
    systemNode.connect(destination);
    void mic.context.close();
    void system.context.close();
    return {
      mode: "mixed",
      context,
      stream: destination.stream,
      inputStreams: [mic.stream, system.stream],
      sourceNodes: [micNode, systemNode],
      destination,
    };
  } catch (error) {
    stopAudioSource(mic);
    stopAudioSource(system);
    throw error;
  }
}

// stopToneSource 停止测试音频流并释放 Web Audio 节点。
function stopAudioSource(source: AudioSource | null): void {
  if (!source) {
    return;
  }
  source.sourceNodes?.forEach((node) => node.disconnect());
  source.stream.getTracks().forEach((track) => track.stop());
  source.inputStreams?.forEach((stream) => {
    stream.getTracks().forEach((track) => track.stop());
  });
  source.oscillator?.stop();
  void source.context.close();
}

// createMeter 为一个音频流创建 RMS 音量读取器。
function createMeter(stream: MediaStream): MeterTarget {
  const context = createAudioContext();
  const source = context.createMediaStreamSource(stream);
  const analyser = context.createAnalyser();
  analyser.fftSize = 256;
  source.connect(analyser);
  return { context, source, analyser, samples: new Uint8Array(analyser.frequencyBinCount) as Uint8Array<ArrayBuffer> };
}

// disposeMeter 释放音量表 AudioContext 和节点。
function disposeMeter(target: MeterTarget | null): void {
  if (!target) {
    return;
  }
  target.source.disconnect();
  void target.context.close();
}

// readMeter 读取音频流 RMS，并映射到 0 到 100。
function readMeter(target: MeterTarget | null): number {
  if (!target) {
    return 0;
  }
  target.analyser.getByteTimeDomainData(target.samples);
  let sum = 0;
  for (const value of target.samples) {
    const normalized = (value - 128) / 128;
    sum += normalized * normalized;
  }
  return Math.round(Math.min(1, Math.sqrt(sum / target.samples.length) * 4) * 100);
}

function needsMicDevice(mode: AudioMode): boolean {
  return mode === "mic" || mode === "mixed";
}

function usesAudioProcessing(mode: AudioMode): boolean {
  return mode === "mic" || mode === "mixed";
}

// App 是一个最小化 api-server WebRTC 探测界面。
function App(): React.ReactElement {
  const [form, setForm] = React.useState<FormState>(defaultForm);
  const [logs, setLogs] = React.useState<LogEntry[]>([]);
  const [wsState, setWSState] = React.useState("closed");
  const [peerState, setPeerState] = React.useState("idle");
  const [iceState, setIceState] = React.useState("idle");
  const [signalingState, setSignalingState] = React.useState("idle");
  const [bytesSent, setBytesSent] = React.useState(0);
  const [packetsSent, setPacketsSent] = React.useState(0);
  const [audioMode, setAudioMode] = React.useState<AudioMode>("tone");
  const [audioProcessing, setAudioProcessing] = React.useState<AudioProcessingState>(defaultAudioProcessing);
  const [audioState, setAudioState] = React.useState("idle");
  const [audioDevices, setAudioDevices] = React.useState<MediaDeviceInfo[]>([]);
  const [selectedDeviceId, setSelectedDeviceId] = React.useState("");
  const [localLevel, setLocalLevel] = React.useState(0);
  const [remoteLevel, setRemoteLevel] = React.useState(0);
  const [remoteTrackCount, setRemoteTrackCount] = React.useState(0);
  const [ttsText, setTTSText] = React.useState("你好，这是一段 React WebRTC TTS 测试。");
  const [ttsVoice, setTTSVoice] = React.useState("101001");
  const [ttsLanguage, setTTSLanguage] = React.useState("zh-CN");
  const [ttsAudioUrl, setTTSAudioUrl] = React.useState("");
  const [asrResults, setASRResults] = React.useState<string[]>([]);
  const [tmtResults, setTMTResults] = React.useState<DisplayLine[]>([]);
  const [llmResults, setLLMResults] = React.useState<DisplayLine[]>([]);

  const socketRef = React.useRef<WebSocket | null>(null);
  const peerRef = React.useRef<RTCPeerConnection | null>(null);
  const audioRef = React.useRef<AudioSource | null>(null);
  const remoteAudioRef = React.useRef<HTMLAudioElement | null>(null);
  const ttsAudioRef = React.useRef<HTMLAudioElement | null>(null);
  const ttsObjectUrlRef = React.useRef("");
  const localMeterRef = React.useRef<MeterTarget | null>(null);
  const remoteMeterRef = React.useRef<MeterTarget | null>(null);
  const pendingRemoteICE = React.useRef<IcePayload[]>([]);
  const statsTimer = React.useRef<number | null>(null);
  const meterTimer = React.useRef<number | null>(null);

  // log 同时写入浏览器控制台和页面事件列表。
  const log = React.useCallback((title: string, payload?: unknown) => {
    const entry = { id: ++logSequence, time: new Date().toLocaleTimeString(), title, payload };
    console.log(`[API Probe] ${title}`, payload ?? "");
    setLogs((current) => [entry, ...current].slice(0, 200));
  }, []);

  // sendWS 发送 WebSocket 消息并打印控制台日志。
  const sendWS = React.useCallback((message: WSMessage) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket 未连接");
    }
    socket.send(JSON.stringify(message));
    log("发送 WebSocket 消息", redactWSMessageForLog(message));
  }, [log]);

  // refreshRTCState 同步 PeerConnection 状态到页面。
  const refreshRTCState = React.useCallback(() => {
    const peer = peerRef.current;
    setPeerState(peer?.connectionState || "idle");
    setIceState(peer?.iceConnectionState || "idle");
    setSignalingState(peer?.signalingState || "idle");
  }, []);

  // stopStats 停止浏览器发送统计轮询。
  const stopStats = React.useCallback(() => {
    if (statsTimer.current !== null) {
      window.clearInterval(statsTimer.current);
      statsTimer.current = null;
    }
  }, []);

  // stopMeters 停止本地/远端音量表并释放 AudioContext。
  const stopMeters = React.useCallback(() => {
    if (meterTimer.current !== null) {
      window.cancelAnimationFrame(meterTimer.current);
      meterTimer.current = null;
    }
    disposeMeter(localMeterRef.current);
    disposeMeter(remoteMeterRef.current);
    localMeterRef.current = null;
    remoteMeterRef.current = null;
    setLocalLevel(0);
    setRemoteLevel(0);
  }, []);

  // startMeters 启动音量表刷新，把本地输入和远端输出的 RMS 写入页面。
  const startMeters = React.useCallback(() => {
    if (meterTimer.current !== null) {
      return;
    }
    const tick = () => {
      setLocalLevel(readMeter(localMeterRef.current));
      setRemoteLevel(readMeter(remoteMeterRef.current));
      meterTimer.current = window.requestAnimationFrame(tick);
    };
    meterTimer.current = window.requestAnimationFrame(tick);
  }, []);

  // attachLocalMeter 绑定本地音频输入音量表。
  const attachLocalMeter = React.useCallback((stream: MediaStream) => {
    disposeMeter(localMeterRef.current);
    localMeterRef.current = createMeter(stream);
    startMeters();
  }, [startMeters]);

  // attachRemoteStream 绑定远端音频播放和远端音量表。
  const attachRemoteStream = React.useCallback((stream: MediaStream) => {
    setRemoteTrackCount(stream.getAudioTracks().length || stream.getTracks().length);
    if (remoteAudioRef.current) {
      remoteAudioRef.current.srcObject = stream;
      void remoteAudioRef.current.play().catch((error) => {
        if (isMediaAbortError(error)) {
          console.debug("[API Probe] 远端音频播放被浏览器中断", error);
          return;
        }
        log("远端音频自动播放失败", formatMediaPlayError(error));
      });
    }
    disposeMeter(remoteMeterRef.current);
    remoteMeterRef.current = createMeter(stream);
    startMeters();
  }, [log, startMeters]);

  // playTTSAudio 兼容旧调试协议中通过 WebSocket 返回的 TTS 音频。
  const playTTSAudio = React.useCallback((message: WSMessage) => {
    if (!message.audio) {
      return;
    }
    if (ttsObjectUrlRef.current) {
      URL.revokeObjectURL(ttsObjectUrlRef.current);
      ttsObjectUrlRef.current = "";
    }
    const blob = base64ToBlob(message.audio, audioMimeType(message.format));
    const url = URL.createObjectURL(blob);
    ttsObjectUrlRef.current = url;
    setTTSAudioUrl(url);
    log("收到 TTS 音频", {
      format: message.format,
      sampleRate: message.sampleRate,
      sequence: message.sequence,
      isLast: message.isLast,
      audioBase64Bytes: message.audio.length,
      blobBytes: blob.size,
    });
    if (ttsAudioRef.current) {
      ttsAudioRef.current.src = url;
      void ttsAudioRef.current.play().catch((error) => {
        if (isMediaAbortError(error)) {
          console.debug("[API Probe] TTS 音频播放被浏览器中断", error);
          return;
        }
        log("TTS 音频自动播放失败", formatMediaPlayError(error));
      });
    }
  }, [log]);

  React.useEffect(() => () => {
    if (ttsObjectUrlRef.current) {
      URL.revokeObjectURL(ttsObjectUrlRef.current);
    }
  }, []);

  // refreshAudioDevices 刷新浏览器音频输入设备列表。
  const refreshAudioDevices = React.useCallback(async () => {
    if (!navigator.mediaDevices?.enumerateDevices) {
      throw new Error("浏览器不支持音频设备枚举");
    }
    const devices = await navigator.mediaDevices.enumerateDevices();
    const audioInputs = devices.filter((device) => device.kind === "audioinput");
    setAudioDevices(audioInputs);
    log("音频输入设备已刷新", audioInputs.map((device) => ({
      deviceId: device.deviceId,
      label: device.label || "未授权设备名",
      groupId: device.groupId,
    })));
  }, [log]);

  // ensureAudioSource 按当前模式创建本地音频流，支持生成音、麦克风、系统声音和混合输入。
  const ensureAudioSource = React.useCallback(async () => {
    if (audioRef.current) {
      return audioRef.current;
    }
    setAudioState("starting");
    let source: AudioSource;
    switch (audioMode) {
      case "mic":
        source = await createMicSource(selectedDeviceId, audioProcessing);
        break;
      case "system":
        source = await createSystemSource();
        break;
      case "mixed":
        source = await createMixedSource(selectedDeviceId, audioProcessing);
        break;
      default:
        source = createToneSource();
        break;
    }
    audioRef.current = source;
    attachLocalMeter(source.stream);
    setAudioState(source.mode);
    log("本地音频源已启动", {
      mode: source.mode,
      audioProcessing: usesAudioProcessing(source.mode) ? audioProcessing : undefined,
      inputStreams: source.inputStreams?.map((stream) => ({
        audioTracks: stream.getAudioTracks().length,
        videoTracks: stream.getVideoTracks().length,
      })),
      tracks: source.stream.getAudioTracks().map((track) => ({
        id: track.id,
        label: track.label,
        kind: track.kind,
        enabled: track.enabled,
        muted: track.muted,
        readyState: track.readyState,
        settings: track.getSettings(),
      })),
    });
    if (needsMicDevice(source.mode)) {
      await refreshAudioDevices().catch((error) => log("麦克风授权后刷新设备失败", String(error)));
    }
    return source;
  }, [attachLocalMeter, audioMode, audioProcessing, log, refreshAudioDevices, selectedDeviceId]);

  // startStats 轮询 outbound-rtp 音频统计，确认 RTP 字节数是否增长。
  const startStats = React.useCallback(() => {
    if (statsTimer.current !== null) {
      return;
    }
    statsTimer.current = window.setInterval(async () => {
      const peer = peerRef.current;
      if (!peer) {
        return;
      }
      const sender = peer.getSenders().find((item) => item.track?.kind === "audio");
      const report = sender ? await sender.getStats() : await peer.getStats();
      report.forEach((stat) => {
        const item = stat as RTCStats & { kind?: string; mediaType?: string; bytesSent?: number; packetsSent?: number };
        if (item.type === "outbound-rtp" && (item.kind === "audio" || item.mediaType === "audio")) {
          setBytesSent(item.bytesSent || 0);
          setPacketsSent(item.packetsSent || 0);
          console.log("[API Probe] 浏览器 RTP 发送统计", {
            bytesSent: item.bytesSent || 0,
            packetsSent: item.packetsSent || 0,
            peer: peer.connectionState,
            ice: peer.iceConnectionState,
          });
        }
      });
    }, 1000);
  }, []);

  // applyRemoteICE 应用 api-server 转发的 PBX ICE candidate。
  const applyRemoteICE = React.useCallback(async (candidate: IcePayload) => {
    const peer = peerRef.current;
    if (!peer) {
      pendingRemoteICE.current.push(candidate);
      log("缓存远端 ICE：PeerConnection 尚未创建", summarizeCandidate(candidate.candidate));
      return;
    }
    if (!peer.remoteDescription) {
      pendingRemoteICE.current.push(candidate);
      log("缓存远端 ICE：answer 尚未应用", summarizeCandidate(candidate.candidate));
      return;
    }
    await peer.addIceCandidate(candidate);
    log("应用远端 ICE", summarizeCandidate(candidate.candidate));
  }, [log]);

  // flushRemoteICE 在 answer 应用后批量应用之前缓存的 ICE。
  const flushRemoteICE = React.useCallback(async () => {
    const candidates = pendingRemoteICE.current.splice(0);
    if (candidates.length > 0) {
      log("开始应用缓存 ICE", { count: candidates.length });
    }
    for (const candidate of candidates) {
      await applyRemoteICE(candidate);
    }
  }, [applyRemoteICE, log]);

  // handleWSMessage 处理 api-server 发来的信令、ASR、翻译和 TTS 状态消息。
  const handleWSMessage = React.useCallback(async (message: WSMessage) => {
    log("收到 WebSocket 消息", message);
    if (message.type === "webrtc_answer" && message.sdp) {
      const peer = peerRef.current;
      if (!peer) {
        throw new Error("收到 answer 时 PeerConnection 不存在");
      }
      await peer.setRemoteDescription({ type: "answer", sdp: message.sdp });
      log("应用 WebRTC answer", { sdpBytes: message.sdp.length });
      refreshRTCState();
      await flushRemoteICE();
      return;
    }
    if (message.type === "ice" && message.candidate) {
      await applyRemoteICE(parseICE(message.candidate));
      return;
    }
    if (message.type === "asr_result" && message.text) {
      setASRResults((current) => [
        `[${new Date().toLocaleTimeString()}] ${message.isFinal ? "final" : "partial"} ${message.confidence ?? ""} ${message.text}`,
        ...current,
      ].slice(0, 30));
      return;
    }
    if (isTranslationMessage(message.type) && message.text) {
      const key = message.utteranceId || `translation-${Date.now()}`;
      const line = {
        key,
        value: formatTranslationLine(message),
      };
      const update = (current: DisplayLine[]) => [
        line,
        ...current.filter((item) => item.key !== key),
      ].slice(0, 30);
      if (translationBucket(message) === "tmt") {
        setTMTResults(update);
        return;
      }
      setLLMResults(update);
      return;
    }
    if (message.type === "tts_result") {
      log("TTS 已进入 WebRTC 下行通道", message.metadata);
      return;
    }
    if (message.type === "tts_audio" && message.audio) {
      playTTSAudio(message);
      return;
    }
    if (message.type === "error") {
      console.error("[API Probe] 后端返回错误", message);
    }
  }, [applyRemoteICE, flushRemoteICE, log, playTTSAudio, refreshRTCState]);

  // connectWS 建立 api-server WebSocket 并发送 client_hello。
  const connectWS = React.useCallback(() => {
    if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
      return;
    }
    const socket = new WebSocket(form.wsUrl);
    socketRef.current = socket;
    setWSState("connecting");
    log("开始连接 WebSocket", { url: form.wsUrl });

    socket.addEventListener("open", () => {
      setWSState("open");
      log("WebSocket 已连接");
      sendWS({
        type: "client_hello",
        requestId: requestID("hello"),
        tenantId: form.tenantId,
        extension: form.clientId,
        clientId: form.clientId,
        responseMode: form.responseMode,
        metadata: {
          translateStrategy: form.translateStrategy,
          dubbing: form.dubbing ? "1" : "0",
        },
      });
    });
    socket.addEventListener("message", (event) => {
      void handleWSMessage(JSON.parse(event.data as string) as WSMessage).catch((error) => log("处理 WebSocket 消息失败", String(error)));
    });
    socket.addEventListener("error", (event) => {
      setWSState("error");
      log("WebSocket 连接错误", event);
    });
    socket.addEventListener("close", (event) => {
      setWSState("closed");
      log("WebSocket 已关闭", { code: event.code, reason: event.reason });
    });
  }, [form, handleWSMessage, log, sendWS]);

  // ensurePeer 创建 RTCPeerConnection，绑定 ICE/状态/track 事件，并添加本地音频轨道。
  const ensurePeer = React.useCallback(async () => {
    if (peerRef.current) {
      return peerRef.current;
    }
    const peer = new RTCPeerConnection({
      iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
    });
    peerRef.current = peer;
    pendingRemoteICE.current = [];
    const audioSource = await ensureAudioSource();
    for (const track of audioSource.stream.getAudioTracks()) {
      peer.addTrack(track, audioSource.stream);
    }
    log("创建 PeerConnection 并添加本地音频轨道", {
      audioMode: audioSource.mode,
      audioTracks: audioSource.stream.getAudioTracks().map((track) => ({
        id: track.id,
        kind: track.kind,
        label: track.label,
        enabled: track.enabled,
        readyState: track.readyState,
      })),
    });

    peer.addEventListener("icecandidate", (event) => {
      if (!event.candidate) {
        log("浏览器 ICE 收集完成");
        return;
      }
      const payload: IcePayload = {
        candidate: event.candidate.candidate,
        sdpMid: event.candidate.sdpMid,
        sdpMLineIndex: event.candidate.sdpMLineIndex,
      };
      log("浏览器本地 ICE candidate", summarizeCandidate(payload.candidate));
      sendWS({
        type: "ice",
        requestId: requestID("ice"),
        callId: form.callId,
        userId: form.userId,
        candidate: JSON.stringify(payload),
      });
    });

    peer.addEventListener("icecandidateerror", (event) => {
      const error = event as Event & { address?: string; port?: number; url?: string; errorCode?: number; errorText?: string };
      log("浏览器 ICE candidate 错误", {
        address: error.address,
        port: error.port,
        url: error.url,
        errorCode: error.errorCode,
        errorText: error.errorText,
      });
    });

    peer.addEventListener("track", (event) => {
      const stream = event.streams[0] || new MediaStream([event.track]);
      attachRemoteStream(stream);
      log("收到远端媒体轨道", {
        streams: event.streams.length,
        track: {
          id: event.track.id,
          kind: event.track.kind,
          muted: event.track.muted,
          readyState: event.track.readyState,
        },
      });
    });

    const onState = (name: string) => {
      refreshRTCState();
      log("WebRTC 状态变化", {
        event: name,
        signaling: peer.signalingState,
        ice: peer.iceConnectionState,
        peer: peer.connectionState,
        gathering: peer.iceGatheringState,
      });
      if (peer.connectionState === "connected" || peer.iceConnectionState === "connected") {
        startStats();
      }
    };
    peer.addEventListener("connectionstatechange", () => onState("connectionstatechange"));
    peer.addEventListener("iceconnectionstatechange", () => onState("iceconnectionstatechange"));
    peer.addEventListener("signalingstatechange", () => onState("signalingstatechange"));
    peer.addEventListener("icegatheringstatechange", () => onState("icegatheringstatechange"));
    refreshRTCState();
    return peer;
  }, [attachRemoteStream, ensureAudioSource, form.callId, form.userId, log, refreshRTCState, sendWS, startStats]);

  // startWebRTC 创建 offer 并通过 api-server WebSocket 发送信令。
  const startWebRTC = React.useCallback(async () => {
    const peer = await ensurePeer();
    const offer = await peer.createOffer({ offerToReceiveAudio: true });
    await peer.setLocalDescription(offer);
    refreshRTCState();
    log("创建并设置本地 offer", { sdpBytes: offer.sdp?.length || 0 });
    sendWS({
      type: "webrtc_offer",
      requestId: requestID("offer"),
      callId: form.callId,
      userId: form.userId,
      sdp: offer.sdp || "",
      metadata: { source: "react-api-probe", media: audioRef.current?.mode || audioMode },
    });
  }, [audioMode, ensurePeer, form.callId, form.userId, log, refreshRTCState, sendWS]);

  // sendTTSCommand 发送 TTS 调试命令给 api-server，由 api-server 转发给 PBX。
  const sendTTSCommand = React.useCallback(() => {
    sendWS({
      type: "tts_command",
      requestId: requestID("tts"),
      callId: form.callId,
      userId: form.userId,
      text: ttsText,
      voice: ttsVoice,
      language: ttsLanguage,
    });
  }, [form.callId, form.userId, sendWS, ttsLanguage, ttsText, ttsVoice]);

  // stopAll 关闭 WebSocket、PeerConnection 和测试音频。
  const stopAll = React.useCallback(() => {
    stopStats();
    if (peerRef.current) {
      peerRef.current.close();
      peerRef.current = null;
    }
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
    stopAudioSource(audioRef.current);
    audioRef.current = null;
    if (ttsObjectUrlRef.current) {
      URL.revokeObjectURL(ttsObjectUrlRef.current);
      ttsObjectUrlRef.current = "";
      setTTSAudioUrl("");
    }
    stopMeters();
    pendingRemoteICE.current = [];
    setBytesSent(0);
    setPacketsSent(0);
    setAudioState("idle");
    setRemoteTrackCount(0);
    refreshRTCState();
    log("已停止探测连接");
  }, [log, refreshRTCState, stopMeters, stopStats]);

  const updateForm = (key: TextFormKey, value: string) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  const updateStrategy = (value: string) => {
    setForm((current) => ({ ...current, translateStrategy: value }));
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      sendWS({
        type: "set_strategy",
        requestId: requestID("strategy"),
        metadata: { translateStrategy: value },
      });
    }
  };

  const updateDubbing = (value: boolean) => {
    setForm((current) => ({ ...current, dubbing: value }));
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      sendWS({
        type: "set_dubbing",
        requestId: requestID("dubbing"),
        metadata: { dubbing: value ? "1" : "0" },
      });
    }
  };

  const updateAudioProcessing = (key: keyof AudioProcessingState, value: boolean) => {
    setAudioProcessing((current) => ({ ...current, [key]: value }));
  };

  return (
    <main className="app">
      <section className="workspace">
        <header className="topbar">
          <div>
            <h1>API Server WebRTC Probe</h1>
            <p>连接 api-server，观察 WebSocket 信令、WebRTC 媒体和 AI 字幕链路</p>
          </div>
          <div className="status-grid">
            <Status label="ws" value={wsState} />
            <Status label="peer" value={peerState} />
            <Status label="ice" value={iceState} />
            <Status label="signaling" value={signalingState} />
          </div>
        </header>

        <section className="control-band">
          <label>
            WebSocket
            <input value={form.wsUrl} onChange={(event) => updateForm("wsUrl", event.target.value)} />
          </label>
          <label>
            Tenant
            <input value={form.tenantId} onChange={(event) => updateForm("tenantId", event.target.value)} />
          </label>
          <label>
            Client
            <input value={form.clientId} onChange={(event) => updateForm("clientId", event.target.value)} />
          </label>
          <label>
            Mode
            <select value={form.responseMode} onChange={(event) => updateForm("responseMode", event.target.value)}>
              <option value="debug">debug</option>
              <option value="compact">compact</option>
            </select>
          </label>
          <label>
            Strategy
            <select value={form.translateStrategy} onChange={(event) => updateStrategy(event.target.value)}>
              <option value="tmt">tmt</option>
              <option value="hybrid">hybrid</option>
              <option value="deepseek">deepseek</option>
              <option value="llm">llm</option>
            </select>
          </label>
          <label className="check-row inline-check">
            <input type="checkbox" checked={form.dubbing} onChange={(event) => updateDubbing(event.target.checked)} />
            Dubbing
          </label>
          <label>
            Call
            <input value={form.callId} onChange={(event) => updateForm("callId", event.target.value)} />
          </label>
          <label>
            User
            <input value={form.userId} onChange={(event) => updateForm("userId", event.target.value)} />
          </label>
        </section>

        <section className="actions-band">
          <button onClick={connectWS} title="连接 api-server WebSocket">
            <Cable size={18} />
            连接 WS
          </button>
          <button className="primary" onClick={() => void startWebRTC().catch((error) => log("启动 WebRTC 失败", String(error)))} title="创建 offer 并开始发送 RTP">
            <Play size={18} />
            建立 WebRTC
          </button>
          <button onClick={stopAll} title="关闭 WebSocket 和 PeerConnection">
            <Square size={18} />
            停止
          </button>
          <button onClick={() => setLogs([])} title="清空页面日志">
            <RotateCcw size={18} />
            清空日志
          </button>
        </section>

        <section className="audio-band">
          <div className="audio-controls">
            <label>
              音频源
              <select value={audioMode} onChange={(event) => setAudioMode(event.target.value as AudioMode)} disabled={peerState !== "idle"}>
                <option value="tone">浏览器生成音</option>
                <option value="mic">麦克风</option>
                <option value="system">系统声音</option>
                <option value="mixed">麦克风 + 系统</option>
              </select>
            </label>
            <label>
              输入设备
              <select value={selectedDeviceId} onChange={(event) => setSelectedDeviceId(event.target.value)} disabled={peerState !== "idle" || !needsMicDevice(audioMode)}>
                <option value="">默认输入</option>
                {audioDevices.map((device) => (
                  <option key={device.deviceId} value={device.deviceId}>
                    {device.label || `音频输入 ${device.deviceId.slice(0, 8)}`}
                  </option>
                ))}
              </select>
            </label>
            <label className="check-row">
              <input type="checkbox" checked={audioProcessing.echoCancellation} onChange={(event) => updateAudioProcessing("echoCancellation", event.target.checked)} disabled={peerState !== "idle" || !usesAudioProcessing(audioMode)} />
              EC
            </label>
            <label className="check-row">
              <input type="checkbox" checked={audioProcessing.noiseSuppression} onChange={(event) => updateAudioProcessing("noiseSuppression", event.target.checked)} disabled={peerState !== "idle" || !usesAudioProcessing(audioMode)} />
              NS
            </label>
            <label className="check-row">
              <input type="checkbox" checked={audioProcessing.autoGainControl} onChange={(event) => updateAudioProcessing("autoGainControl", event.target.checked)} disabled={peerState !== "idle" || !usesAudioProcessing(audioMode)} />
              AGC
            </label>
            <button onClick={() => void refreshAudioDevices().catch((error) => log("刷新音频设备失败", String(error)))} title="刷新音频输入设备">
              <RefreshCw size={18} />
              设备
            </button>
          </div>
          <div className="meter-grid">
            <Meter label="本地输入" value={localLevel} />
            <Meter label="远端输出" value={remoteLevel} />
          </div>
          <audio ref={remoteAudioRef} controls autoPlay />
        </section>

        <section className="metrics-band">
          <Metric icon={<Waves size={18} />} label="bytesSent" value={String(bytesSent)} />
          <Metric icon={<Activity size={18} />} label="packetsSent" value={String(packetsSent)} />
          <Metric icon={<Mic size={18} />} label="audio" value={audioState} />
          <Metric icon={<Volume2 size={18} />} label="remoteTracks" value={String(remoteTrackCount)} />
          <Metric icon={<Phone size={18} />} label="callId" value={form.callId} />
        </section>

        <section className="tts-band">
          <div className="tts-fields">
            <label>
              TTS 文本
              <textarea value={ttsText} onChange={(event) => setTTSText(event.target.value)} />
            </label>
            <label>
              音色
              <input value={ttsVoice} onChange={(event) => setTTSVoice(event.target.value)} />
            </label>
            <label>
              语言
              <input value={ttsLanguage} onChange={(event) => setTTSLanguage(event.target.value)} />
            </label>
          </div>
          <button onClick={sendTTSCommand} title="发送 TTS 调试命令">
            <Volume2 size={18} />
            发送 TTS
          </button>
          <audio ref={ttsAudioRef} src={ttsAudioUrl} controls />
        </section>

        <section className="asr-band">
          <div className="log-head">
            <h2>ASR</h2>
            <span>{asrResults.length}</span>
          </div>
          <div className="asr-list">
            {asrResults.map((item) => (
              <pre key={item}>{item}</pre>
            ))}
          </div>
        </section>

        <section className="translation-band">
          <div className="log-head">
            <h2>翻译</h2>
            <span>TMT {tmtResults.length} / LLM {llmResults.length}</span>
          </div>
          <div className="translation-grid">
            <article className="translation-panel">
              <div className="panel-head">
                <strong>TMT</strong>
                <span>{tmtResults.length}</span>
              </div>
              <div className="asr-list">
                {tmtResults.map((item) => (
                  <pre key={item.key}>{item.value}</pre>
                ))}
              </div>
            </article>
            <article className="translation-panel">
              <div className="panel-head">
                <strong>LLM</strong>
                <span>{llmResults.length}</span>
              </div>
              <div className="asr-list">
                {llmResults.map((item) => (
                  <pre key={item.key}>{item.value}</pre>
                ))}
              </div>
            </article>
          </div>
        </section>

        <section className="log-band">
          <div className="log-head">
            <h2>事件</h2>
            <span>详细信息也会同步打印到浏览器 console</span>
          </div>
          <div className="log-list">
            {logs.map((entry) => (
              <article key={entry.id} className="log-row">
                <div>
                  <strong>{entry.title}</strong>
                  <time>{entry.time}</time>
                </div>
                {entry.payload !== undefined && <pre>{formatPayload(entry.payload)}</pre>}
              </article>
            ))}
          </div>
        </section>
      </section>
    </main>
  );
}

function Status({ label, value }: { label: string; value: string }): React.ReactElement {
  return (
    <div className={`status ${value === "connected" || value === "open" ? "ok" : value === "failed" || value === "error" ? "bad" : ""}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }): React.ReactElement {
  return (
    <div className="metric">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function Meter({ label, value }: { label: string; value: number }): React.ReactElement {
  return (
    <div className="meter-row">
      <span>{label}</span>
      <div className="meter-track">
        <i style={{ width: `${value}%` }} />
      </div>
      <strong>{value}%</strong>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
