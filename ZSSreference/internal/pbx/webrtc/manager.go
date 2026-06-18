// WebRTC管理器(核心大文件~2100行)：AcceptOffer→ 音频管线(Opus解码→ 重采样→ VAD→ ASR)→ TMT快翻→ TTS下行播放→ ICE处理
package webrtc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/asr"
	"github.com/SATA260/SimulSpeak1/internal/ai/tmt"
	"github.com/SATA260/SimulSpeak1/internal/ai/vad"
	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxevents "github.com/SATA260/SimulSpeak1/internal/pbx/events"
	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	pionsdp "github.com/pion/sdp/v3"
	pionwebrtc "github.com/pion/webrtc/v4"
	pionmedia "github.com/pion/webrtc/v4/pkg/media"
	"layeh.com/gopus"
)

const (
	audioLogEveryPackets          = 50
	audioLogEveryDuration         = 5 * time.Second
	audioWaitTimeout              = 10 * time.Second
	defaultUDPPortMin             = 20000
	defaultUDPPortMax             = 20100
	defaultIncludeLoopbackICEHost = true
)

type ICECallback func(candidate string)
type ASRResultCallback func(result model.ASRResult)
type TranslationResultCallback func(result model.TranslationResult)

type OfferRequest struct {
	ConnectionID        string
	TenantID            string
	CallID              string
	UserID              string
	SDP                 string
	Metadata            map[string]string
	ProviderConfigs     map[model.CapabilityType]model.ProviderConfig
	OnICE               ICECallback
	OnASRResult         ASRResultCallback
	OnTranslationResult TranslationResultCallback
}

type Session struct {
	connectionID        string
	tenantID            string
	callID              string
	userID              string
	peer                *pionwebrtc.PeerConnection
	createdAt           time.Time
	candidateMu         sync.Mutex
	localCandidates     []iceCandidateSummary
	remoteCandidates    []iceCandidateSummary
	audioTrackSeen      atomic.Bool
	audioPacketSeen     atomic.Bool
	audioWatchStarted   atomic.Bool
	asrClient           *asr.Client
	asrDisabledReason   string
	asrMu               sync.Mutex
	asrStream           asr.Stream
	asrStreamDisabled   bool
	utteranceSeq        int
	asrProcessor        *asrAudioProcessor
	asrPreparedFrames   atomic.Int64
	asrPreparedBytes    atomic.Int64
	asrWrittenFrames    atomic.Int64
	asrWrittenBytes     atomic.Int64
	asrSkippedFrames    atomic.Int64
	asrSkippedBytes     atomic.Int64
	asrResultMu         sync.Mutex
	asrForwardPartial   bool
	asrForwarded        map[string]struct{}
	asrGate             *asrVoiceGate
	recordingMu         sync.Mutex
	recording           *recording.PCM16WAVRecorder
	recordingProcessor  *asrAudioProcessor
	recordingSampleRate int
	recordingStarted    recording.Metadata
	eventBus            *eventbus.MemoryBus
	asrProvider         string
	sourceLanguage      string
	onASRResult         ASRResultCallback
	tmtClient           *tmt.Client
	tmtMu               sync.Mutex
	tmtForwarded        map[string]struct{}
	tmtProvider         string
	targetLanguage      string
	onTranslation       TranslationResultCallback
	ttsTrack            *pionwebrtc.TrackLocalStaticSample
	ttsMu               sync.Mutex
	ttsQueue            []encodedAudioFrame
	ttsPlaying          bool
	ttsOrderedNext      int
	ttsOrderedReady     map[int][]encodedAudioFrame
	ttsOrderedSkipped   map[int]struct{}
}

type Manager struct {
	mu                  sync.Mutex
	sessions            map[string]*Session
	vadDetector         *vad.Detector
	eventBus            *eventbus.MemoryBus
	providerConfigs     map[model.CapabilityType]model.ProviderConfig
	udpPortMin          int
	udpPortMax          int
	recordingService    *recording.Service
	recordingEnabled    bool
	recordingSampleRate int
}

type ManagerOption func(*Manager)

type PlaybackChunk struct {
	CallID     string
	Payload    []byte
	Format     string
	SampleRate int
	Sequence   int
}

type PlaybackResult struct {
	Queued     bool
	Chunks     int
	AudioBytes int
	FrameCount int
	Sequence   int
}

type encodedAudioFrame struct {
	payload  []byte
	duration time.Duration
}

type asrAudioProcessor struct {
	mu          sync.Mutex
	opusDecoder *gopus.Decoder
}

type asrVoiceGate struct {
	enabled           bool
	vadSessionID      string
	detector          *vad.Detector
	detectorThreshold float64
	threshold         float64
	startFrames       int
	endSilenceFrames  int
	preRollFrames     int
	speaking          bool
	speechFrames      int
	silenceFrames     int
	droppedFrames     int
	droppedBytes      int
	preRoll           [][]byte
}

type asrVoiceGateDecision struct {
	WriteFrames   [][]byte
	SpeechStart   bool
	SpeechEnd     bool
	Speech        bool
	Energy        float64
	VADConfidence float64
	VADThreshold  float64
	VADSource     string
	DroppedFrames int
	DroppedBytes  int
}

// NewManager 创建 WebRTC 会话管理器。
func NewManager(options ...ManagerOption) *Manager {
	manager := &Manager{
		sessions:            map[string]*Session{},
		udpPortMin:          defaultUDPPortMin,
		udpPortMax:          defaultUDPPortMax,
		recordingSampleRate: 16000,
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

// WithVADDetector 注入 PBX 侧 VAD detector，用于在 WebRTC 上行 ASR 前做模型级语音活动检测。
func WithVADDetector(detector *vad.Detector) ManagerOption {
	return func(manager *Manager) {
		manager.vadDetector = detector
	}
}

// WithICEPortRange 配置本 PBX 节点用于 WebRTC ICE 的 UDP 端口范围。
func WithICEPortRange(minPort, maxPort int) ManagerOption {
	return func(manager *Manager) {
		if minPort <= 0 || maxPort <= 0 || maxPort < minPort || minPort > 65535 || maxPort > 65535 {
			return
		}
		manager.udpPortMin = minPort
		manager.udpPortMax = maxPort
	}
}

// WithProviderConfigs 注入服务端默认 ASR/TTS provider 配置。
func WithProviderConfigs(configs map[model.CapabilityType]model.ProviderConfig) ManagerOption {
	return func(manager *Manager) {
		manager.providerConfigs = model.CloneProviderConfigs(configs)
	}
}

// WithEventBus 注入 PBX 进程内事件总线，用于把 WebRTC/ASR/TMT/TTS 结果交给 control adapter。
func WithEventBus(bus *eventbus.MemoryBus) ManagerOption {
	return func(manager *Manager) {
		manager.eventBus = bus
	}
}

// WithRecording 注入 PBX 侧录音服务；enabled=false 时不会触碰音频热路径。
func WithRecording(service *recording.Service, enabled bool, sampleRate int) ManagerOption {
	return func(manager *Manager) {
		manager.recordingService = service
		manager.recordingEnabled = enabled
		if sampleRate <= 0 {
			sampleRate = 16000
		}
		manager.recordingSampleRate = sampleRate
	}
}

// SetEventBus 在 Manager 创建后绑定 PBX 进程内事件总线。
func (m *Manager) SetEventBus(bus *eventbus.MemoryBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = bus
	for _, session := range m.sessions {
		session.eventBus = bus
	}
}

// AcceptOffer 创建或替换指定连接上的 WebRTC PeerConnection，并返回本端 answer SDP。
// 逻辑: 关闭旧会话，创建 Pion PeerConnection，绑定连接状态/ICE/音频 track 回调，应用远端 offer 后生成本端 answer。
func (m *Manager) AcceptOffer(ctx context.Context, req OfferRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if req.OnICE == nil {
		req.OnICE = func(string) {}
	}

	m.CloseConnection(req.ConnectionID)
	api, err := m.newWebRTCAPI(ctx)
	if err != nil {
		return "", err
	}
	peer, err := api.NewPeerConnection(pionwebrtc.Configuration{
		ICEServers: []pionwebrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return "", err
	}
	ttsTrack, sender, err := addTTSTrack(peer)
	if err != nil {
		_ = peer.Close()
		return "", err
	}
	languageOptions, err := model.NormalizeSessionLanguageOptions(req.Metadata)
	if err != nil {
		_ = peer.Close()
		return "", err
	}
	providerConfigs := model.ApplySessionLanguageOptionsToProviderConfigs(model.MergeProviderConfigs(m.providerConfigs, req.ProviderConfigs), languageOptions)
	asrClient := newASRClientFromProviderConfig(providerConfigs)
	tmtClient := newTMTClientFromProviderConfig(providerConfigs)
	asrDisabledReason := ""
	if asrClient == nil {
		asrDisabledReason = "asr_provider_not_configured"
	}
	asrProcessor, err := newASRAudioProcessorFromProviderConfig(providerConfigs)
	if err != nil {
		asrDisabledReason = "asr_audio_processor_init_failed"
		slog.ErrorContext(ctx, "WebRTC ASR 音频处理器初始化失败，本次会话已禁用 ASR",
			slog.String("connectionId", req.ConnectionID),
			slog.String("callId", req.CallID),
			slog.String("userId", req.UserID),
			slog.String("asrProvider", providerConfigs[model.CapabilityTypeASR].Provider),
			slog.Bool("asrEndpointConfigured", providerConfigs[model.CapabilityTypeASR].Endpoint != ""),
			slog.Any("error", err),
		)
		asrClient = nil
	}

	asrGate := newASRVoiceGateFromProviderConfig(providerConfigs)
	if asrGate != nil && m.vadDetector != nil {
		asrGate.vadSessionID = req.ConnectionID
		asrGate.detector = m.vadDetector
		asrGate.detectorThreshold = m.vadDetector.SpeechThreshold()
	}
	m.logMediaPipelineConfig(ctx, req, providerConfigs, asrClient, asrGate, asrDisabledReason)
	recorder, recordingProcessor, recordingStarted := m.startSessionRecording(ctx, req)

	session := &Session{
		connectionID:        req.ConnectionID,
		tenantID:            req.TenantID,
		callID:              req.CallID,
		userID:              req.UserID,
		peer:                peer,
		createdAt:           time.Now(),
		asrClient:           asrClient,
		asrDisabledReason:   asrDisabledReason,
		asrProcessor:        asrProcessor,
		asrForwardPartial:   shouldForwardASRPartial(providerConfigs) || tmtClient != nil,
		asrForwarded:        map[string]struct{}{},
		asrGate:             asrGate,
		recording:           recorder,
		recordingProcessor:  recordingProcessor,
		recordingSampleRate: m.recordingSampleRate,
		recordingStarted:    recordingStarted,
		eventBus:            m.eventBus,
		asrProvider:         providerConfigs[model.CapabilityTypeASR].Provider,
		sourceLanguage:      languageOptions.SourceLanguage,
		onASRResult:         req.OnASRResult,
		tmtClient:           tmtClient,
		tmtForwarded:        map[string]struct{}{},
		tmtProvider:         providerConfigs[model.CapabilityTypeTMT].Provider,
		targetLanguage:      languageOptions.TargetLanguage,
		onTranslation:       req.OnTranslationResult,
		ttsTrack:            ttsTrack,
	}
	m.storeSession(session)
	go discardSenderRTCP(ctx, sender)
	bindPeerCallbacks(ctx, session, req.OnICE)

	if err := peer.SetRemoteDescription(pionwebrtc.SessionDescription{Type: pionwebrtc.SDPTypeOffer, SDP: req.SDP}); err != nil {
		m.CloseConnection(req.ConnectionID)
		return "", err
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		m.CloseConnection(req.ConnectionID)
		return "", err
	}
	if err := peer.SetLocalDescription(answer); err != nil {
		m.CloseConnection(req.ConnectionID)
		return "", err
	}
	slog.InfoContext(ctx, "WebRTC answer 已创建",
		slog.String("connectionId", req.ConnectionID),
		slog.String("callId", req.CallID),
		slog.String("userId", req.UserID),
		slog.Int("answerBytes", len(answer.SDP)),
		slog.Any("offerAudio", summarizeAudioSDP(req.SDP)),
		slog.Any("answerAudio", summarizeAudioSDP(answer.SDP)),
	)
	return answer.SDP, nil
}

func (m *Manager) logMediaPipelineConfig(ctx context.Context, req OfferRequest, providerConfigs map[model.CapabilityType]model.ProviderConfig, asrClient *asr.Client, asrGate *asrVoiceGate, asrDisabledReason string) {
	asrConfig := providerConfigs[model.CapabilityTypeASR]
	tmtConfig := providerConfigs[model.CapabilityTypeTMT]
	ttsConfig := providerConfigs[model.CapabilityTypeTTS]
	slog.InfoContext(ctx, "WebRTC 媒体链路配置已应用",
		slog.String("connectionId", req.ConnectionID),
		slog.String("callId", req.CallID),
		slog.String("userId", req.UserID),
		slog.Bool("asrEnabled", asrClient != nil),
		slog.String("asrDisabledReason", asrDisabledReason),
		slog.String("asrProvider", asrConfig.Provider),
		slog.Bool("asrEndpointConfigured", asrConfig.Endpoint != ""),
		slog.Bool("asrAppIdConfigured", strings.TrimSpace(asrConfig.Params["appId"]) != ""),
		slog.Bool("asrSecretIdConfigured", strings.TrimSpace(asrConfig.Secrets["secretId"]) != ""),
		slog.Bool("asrSecretKeyConfigured", strings.TrimSpace(asrConfig.Secrets["secretKey"]) != ""),
		slog.Bool("asrStreamingSupported", asrClient != nil && asrClient.SupportsStreaming()),
		slog.String("sourceLanguage", asrConfig.Params["language"]),
		slog.String("asrEngineType", asrConfig.Params["engine_model_type"]),
		slog.Bool("vadGateConfigured", asrGate != nil),
		slog.Bool("vadGateEnabled", asrGate != nil && asrGate.enabled),
		slog.Bool("sileroVADAttached", m.vadDetector != nil),
		slog.Bool("tmtEnabled", strings.TrimSpace(tmtConfig.Provider) != ""),
		slog.String("tmtProvider", tmtConfig.Provider),
		slog.String("tmtSourceLanguage", tmtConfig.Params["source"]),
		slog.String("tmtTargetLanguage", tmtConfig.Params["target"]),
		slog.Bool("tmtEndpointConfigured", tmtConfig.Endpoint != ""),
		slog.Bool("tmtSecretIdConfigured", strings.TrimSpace(tmtConfig.Secrets["secretId"]) != ""),
		slog.Bool("tmtSecretKeyConfigured", strings.TrimSpace(tmtConfig.Secrets["secretKey"]) != ""),
		slog.String("ttsProvider", ttsConfig.Provider),
		slog.String("ttsLanguage", ttsConfig.Params["language"]),
		slog.String("ttsVoiceType", ttsConfig.Params["voiceType"]),
		slog.Bool("ttsEndpointConfigured", ttsConfig.Endpoint != ""),
	)
}

func (m *Manager) startSessionRecording(ctx context.Context, req OfferRequest) (*recording.PCM16WAVRecorder, *asrAudioProcessor, recording.Metadata) {
	if !m.recordingEnabledForOffer(req) {
		return nil, nil, recording.Metadata{}
	}
	if m.recordingService == nil {
		slog.WarnContext(ctx, "WebRTC 录音已请求但 recording service 未配置，已跳过本次录音",
			slog.String("connectionId", req.ConnectionID),
			slog.String("callId", req.CallID),
			slog.String("userId", req.UserID),
		)
		return nil, nil, recording.Metadata{}
	}
	processor, err := newOpusToPCM16Processor()
	if err != nil {
		slog.WarnContext(ctx, "WebRTC 录音 Opus 解码器初始化失败，已跳过本次录音",
			slog.String("connectionId", req.ConnectionID),
			slog.String("callId", req.CallID),
			slog.String("userId", req.UserID),
			slog.Any("error", err),
		)
		return nil, nil, recording.Metadata{}
	}
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		tenantID = "default"
	}
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		callID = req.ConnectionID
	}
	recorder, metadata, err := recording.StartPCM16WAVRecorder(ctx, m.recordingService, recording.PCM16WAVConfig{
		TenantID:    tenantID,
		CallID:      callID,
		RecordingID: idgen.RecordingID(),
		SampleRate:  m.recordingSampleRate,
	})
	if err != nil {
		slog.WarnContext(ctx, "WebRTC 录音启动失败，已跳过本次录音",
			slog.String("connectionId", req.ConnectionID),
			slog.String("callId", req.CallID),
			slog.String("userId", req.UserID),
			slog.Any("error", err),
		)
		return nil, nil, recording.Metadata{}
	}
	slog.InfoContext(ctx, "WebRTC 录音已启动",
		slog.String("connectionId", req.ConnectionID),
		slog.String("callId", req.CallID),
		slog.String("userId", req.UserID),
		slog.String("tenantId", tenantID),
		slog.String("recordingId", metadata.ID),
		slog.String("objectKey", metadata.ObjectKey),
		slog.Int("sampleRate", m.recordingSampleRate),
	)
	return recorder, processor, metadata
}

func (m *Manager) recordingEnabledForOffer(req OfferRequest) bool {
	if value, ok := req.Metadata["recording"]; ok && strings.TrimSpace(value) != "" {
		return truthyConfigValue(value)
	}
	return m.recordingEnabled
}

// PlayAudio 将 TTS 音频转为 WebRTC 可发送的 PCMU 帧，并放入指定会话的下行播放队列。
func (m *Manager) PlayAudio(ctx context.Context, connectionID string, chunks []PlaybackChunk) (PlaybackResult, error) {
	if err := ctx.Err(); err != nil {
		return PlaybackResult{}, err
	}
	session := m.session(connectionID)
	if session == nil {
		return PlaybackResult{}, errors.New("webrtc session not found")
	}
	slog.InfoContext(ctx, "WebRTC 收到 TTS 下行播放请求",
		slog.String("connectionId", connectionID),
		slog.String("callId", session.callID),
		slog.String("userId", session.userID),
		slog.Int("chunkCount", len(chunks)),
	)
	result := PlaybackResult{Chunks: len(chunks)}
	var frames []encodedAudioFrame
	var sequence int
	for _, chunk := range chunks {
		result.AudioBytes += len(chunk.Payload)
		if chunk.Sequence > 0 {
			if sequence == 0 {
				sequence = chunk.Sequence
			} else if sequence != chunk.Sequence {
				return PlaybackResult{}, errors.New("ordered playback chunks must share one sequence")
			}
		}
		encoded, err := encodePlaybackChunk(chunk)
		if err != nil {
			return PlaybackResult{}, err
		}
		frames = append(frames, encoded...)
	}
	result.FrameCount = len(frames)
	result.Sequence = sequence
	if sequence > 0 {
		result.Queued = session.enqueueOrderedTTSAudio(ctx, sequence, frames)
	} else {
		result.Queued = session.enqueueTTSAudio(ctx, frames)
	}
	slog.InfoContext(ctx, "WebRTC TTS 音频已编码并加入下行队列",
		slog.String("connectionId", connectionID),
		slog.String("callId", session.callID),
		slog.String("userId", session.userID),
		slog.Int("chunkCount", result.Chunks),
		slog.Int("audioBytes", result.AudioBytes),
		slog.Int("pcmuFrames", result.FrameCount),
		slog.Int("sequence", result.Sequence),
		slog.Bool("queued", result.Queued),
	)
	return result, nil
}

// SkipAudioSequence 标记指定有序 TTS 序号已失败/跳过，避免后续已完成音频永久等待。
func (m *Manager) SkipAudioSequence(ctx context.Context, connectionID string, sequence int, reason string) {
	if sequence <= 0 {
		return
	}
	session := m.session(connectionID)
	if session == nil {
		return
	}
	session.skipOrderedTTSAudio(ctx, sequence, reason)
}

// AddICECandidate 将远端 ICE candidate 写入指定连接的 PeerConnection。
func (m *Manager) AddICECandidate(ctx context.Context, connectionID, rawCandidate string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session := m.session(connectionID)
	if session == nil {
		slog.WarnContext(ctx, "WebRTC ICE 已忽略：没有活跃会话",
			slog.String("connectionId", connectionID),
			slog.Int("candidateBytes", len(rawCandidate)),
		)
		return nil
	}
	candidate, err := parseICECandidate(rawCandidate)
	if err != nil {
		return err
	}
	if err := session.peer.AddICECandidate(candidate); err != nil {
		return err
	}
	summary := session.recordRemoteICECandidate(candidate.Candidate)
	slog.InfoContext(ctx, "WebRTC ICE candidate 已添加",
		slog.String("connectionId", connectionID),
		slog.String("callId", session.callID),
		slog.String("userId", session.userID),
		slog.Int("candidateBytes", len(rawCandidate)),
		slog.Any("candidate", summary),
	)
	return nil
}

// addTTSTrack 为 PeerConnection 添加 PBX 到前端的 PCMU 音频轨道，用于承载 TTS 播放。
func addTTSTrack(peer *pionwebrtc.PeerConnection) (*pionwebrtc.TrackLocalStaticSample, *pionwebrtc.RTPSender, error) {
	track, err := pionwebrtc.NewTrackLocalStaticSample(
		pionwebrtc.RTPCodecCapability{MimeType: pionwebrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1},
		"pbx-tts",
		"simulspeak",
	)
	if err != nil {
		return nil, nil, err
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		return nil, nil, err
	}
	return track, sender, nil
}

// discardSenderRTCP 持续读取下行轨道 RTCP，避免 Pion 发送端反馈缓冲阻塞。
func discardSenderRTCP(ctx context.Context, sender *pionwebrtc.RTPSender) {
	buffer := make([]byte, 1500)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}

// newWebRTCAPI 创建带固定 UDP4 端口范围和 loopback candidate 的 Pion API，便于开发/部署时放行 WebRTC 媒体端口。
func (m *Manager) newWebRTCAPI(ctx context.Context) (*pionwebrtc.API, error) {
	var setting pionwebrtc.SettingEngine
	if err := setting.SetEphemeralUDPPortRange(uint16(m.udpPortMin), uint16(m.udpPortMax)); err != nil {
		return nil, err
	}
	setting.SetNetworkTypes([]pionwebrtc.NetworkType{pionwebrtc.NetworkTypeUDP4})
	setting.SetIncludeLoopbackCandidate(defaultIncludeLoopbackICEHost)
	slog.InfoContext(ctx, "WebRTC ICE 配置已应用",
		slog.Int("udpPortMin", m.udpPortMin),
		slog.Int("udpPortMax", m.udpPortMax),
		slog.Bool("includeLoopbackCandidate", defaultIncludeLoopbackICEHost),
		slog.Any("networkTypes", []string{pionwebrtc.NetworkTypeUDP4.String()}),
		slog.Any("iceServers", []string{"stun:stun.l.google.com:19302"}),
	)
	return pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(setting)), nil
}

// CloseConnection 关闭指定 WebSocket 连接对应的 WebRTC 会话。
func (m *Manager) CloseConnection(connectionID string) {
	m.mu.Lock()
	session := m.sessions[connectionID]
	delete(m.sessions, connectionID)
	m.mu.Unlock()
	if session == nil {
		return
	}
	session.closeASRStream(context.Background(), "connection_closed")
	session.closeRecording(context.Background(), "connection_closed")
	session.resetVADState()
	if err := session.peer.Close(); err != nil {
		slog.Warn("WebRTC PeerConnection 关闭失败",
			slog.String("connectionId", connectionID),
			slog.String("callId", session.callID),
			slog.Any("error", err),
		)
	}
	slog.Info("WebRTC 会话已关闭",
		slog.String("connectionId", connectionID),
		slog.String("callId", session.callID),
		slog.Duration("duration", time.Since(session.createdAt)),
	)
	session.publishRequiredEvent(context.Background(), pbxevents.NewSessionClosedEvent(pbxevents.SessionClosedPayload{
		ConnectionID: connectionID,
		CallID:       session.callID,
		UserID:       session.userID,
		Reason:       "connection_closed",
	}))
}

// ActiveConnections 返回当前活跃 WebRTC 会话数，用于 PBX 节点负载上报。
func (m *Manager) ActiveConnections() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// storeSession 保存活跃 WebRTC 会话。
func (m *Manager) storeSession(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.connectionID] = session
}

// session 查询指定连接的 WebRTC 会话。
func (m *Manager) session(connectionID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[connectionID]
}

// bindPeerCallbacks 绑定连接状态、ICE candidate 和音频 track 回调。
func bindPeerCallbacks(ctx context.Context, session *Session, onICE ICECallback) {
	session.peer.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		slog.InfoContext(ctx, "WebRTC 连接状态变化",
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.String("userId", session.userID),
			slog.String("state", state.String()),
		)
		session.publishBestEffortEvent(ctx, pbxevents.NewConnectionStateEvent(pbxevents.StatePayload{
			ConnectionID: session.connectionID,
			CallID:       session.callID,
			UserID:       session.userID,
			State:        state.String(),
		}))
		if state == pionwebrtc.PeerConnectionStateConnected {
			slog.InfoContext(ctx, "WebRTC 连接建立成功",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.String("userId", session.userID),
				slog.Duration("setupDuration", time.Since(session.createdAt)),
			)
			startAudioWatch(ctx, session, "peer_connected")
			session.startTTSDrain(ctx, "peer_connected")
		}
	})
	session.peer.OnICEConnectionStateChange(func(state pionwebrtc.ICEConnectionState) {
		slog.InfoContext(ctx, "WebRTC ICE 连接状态变化",
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.String("state", state.String()),
		)
		session.publishBestEffortEvent(ctx, pbxevents.NewICEConnectionStateEvent(pbxevents.StatePayload{
			ConnectionID: session.connectionID,
			CallID:       session.callID,
			UserID:       session.userID,
			State:        state.String(),
		}))
		if state == pionwebrtc.ICEConnectionStateConnected || state == pionwebrtc.ICEConnectionStateCompleted {
			slog.InfoContext(ctx, "WebRTC ICE 已连通，开始等待媒体音频",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.String("userId", session.userID),
				slog.Duration("setupDuration", time.Since(session.createdAt)),
			)
			startAudioWatch(ctx, session, "ice_"+state.String())
		}
		if state == pionwebrtc.ICEConnectionStateFailed {
			logICEFailure(ctx, session)
		}
	})
	session.peer.OnICECandidate(func(candidate *pionwebrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		init := candidate.ToJSON()
		payload, err := json.Marshal(init)
		if err != nil {
			slog.WarnContext(ctx, "WebRTC 本地 ICE 序列化失败",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.Any("error", err),
			)
			return
		}
		session.recordLocalICECandidate(init.Candidate)
		rawCandidate := string(payload)
		slog.InfoContext(ctx, "WebRTC 本地 ICE candidate 已收集",
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.Int("candidateBytes", len(payload)),
			slog.String("address", candidate.Address),
			slog.Int("port", int(candidate.Port)),
			slog.String("type", candidate.Typ.String()),
			slog.String("protocol", candidate.Protocol.String()),
			slog.String("relatedAddress", candidate.RelatedAddress),
			slog.Int("relatedPort", int(candidate.RelatedPort)),
		)
		session.publishBestEffortEvent(ctx, pbxevents.NewICECandidateEvent(pbxevents.ICECandidatePayload{
			ConnectionID: session.connectionID,
			CallID:       session.callID,
			UserID:       session.userID,
			Candidate:    rawCandidate,
		}))
		onICE(rawCandidate)
	})
	session.peer.OnTrack(func(track *pionwebrtc.TrackRemote, receiver *pionwebrtc.RTPReceiver) {
		slog.InfoContext(ctx, "收到 WebRTC 媒体轨道",
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.String("userId", session.userID),
			slog.String("kind", track.Kind().String()),
			slog.String("codec", track.Codec().MimeType),
			slog.Uint64("ssrc", uint64(track.SSRC())),
		)
		session.publishBestEffortEvent(ctx, pbxevents.NewTrackReceivedEvent(pbxevents.StatePayload{
			ConnectionID: session.connectionID,
			CallID:       session.callID,
			UserID:       session.userID,
			Kind:         track.Kind().String(),
			Codec:        track.Codec().MimeType,
		}))
		if track.Kind() != pionwebrtc.RTPCodecTypeAudio {
			slog.InfoContext(ctx, "WebRTC 非音频轨道已忽略",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.String("kind", track.Kind().String()),
			)
			return
		}
		session.audioTrackSeen.Store(true)
		slog.InfoContext(ctx, "WebRTC 音频轨道已接入，等待前端音频包",
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.String("userId", session.userID),
			slog.String("codec", track.Codec().MimeType),
			slog.Uint64("ssrc", uint64(track.SSRC())),
		)
		go readAudioTrack(ctx, session, track)
	})
}

// readAudioTrack 持续读取前端通过 WebRTC 发送到后端的 RTP 音频包并打印日志。
// 逻辑: 阻塞读取 RTP；首包立即打印，之后按包数或时间间隔打印累计包数和字节数；读取结束时打印收尾统计。
func readAudioTrack(ctx context.Context, session *Session, track *pionwebrtc.TrackRemote) {
	var packets int
	var payloadBytes int
	started := time.Now()
	// lastLogged := started
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			slog.InfoContext(ctx, "WebRTC 音频轨道读取已停止",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.String("userId", session.userID),
				slog.Int("packets", packets),
				slog.Int("audioBytes", payloadBytes),
				slog.Duration("duration", time.Since(started)),
				slog.Any("error", err),
			)
			session.closeASRStream(ctx, "audio_track_stopped")
			session.closeRecording(ctx, "audio_track_stopped")
			return
		}
		packets++
		payloadBytes += len(packet.Payload)
		session.queueASRFrame(ctx, packet.Payload, track.Codec().MimeType)
		if packets == 1 {
			session.audioPacketSeen.Store(true)
			// lastLogged = time.Now()
			slog.InfoContext(ctx, "收到首个前端 WebRTC 音频包",
				slog.String("connectionId", session.connectionID),
				slog.String("callId", session.callID),
				slog.String("userId", session.userID),
				slog.Int("packets", packets),
				slog.Int("audioBytes", payloadBytes),
				slog.Int("packetBytes", len(packet.Payload)),
				slog.Int("payloadType", int(packet.PayloadType)),
				slog.Int("sequenceNumber", int(packet.SequenceNumber)),
				slog.Uint64("rtpTimestamp", uint64(packet.Timestamp)),
				slog.Uint64("ssrc", uint64(packet.SSRC)),
			)
			continue
		}
		// if packets%audioLogEveryPackets == 0 || time.Since(lastLogged) >= audioLogEveryDuration {
		// 	lastLogged = time.Now()
		// 	slog.InfoContext(ctx, "持续收到前端 WebRTC 音频包",
		// 		slog.String("connectionId", session.connectionID),
		// 		slog.String("callId", session.callID),
		// 		slog.String("userId", session.userID),
		// 		slog.Int("packets", packets),
		// 		slog.Int("audioBytes", payloadBytes),
		// 		slog.Int("lastPacketBytes", len(packet.Payload)),
		// 		slog.String("codec", track.Codec().MimeType),
		// 		slog.Duration("duration", time.Since(started)),
		// 	)
		// }
	}
}

// enqueueTTSAudio 将已编码的 TTS 音频帧加入下行队列；连接已建立时会启动异步播放。
func (s *Session) enqueueTTSAudio(ctx context.Context, frames []encodedAudioFrame) bool {
	if len(frames) == 0 {
		return false
	}
	s.ttsMu.Lock()
	queued, shouldStart := s.appendTTSAudioLocked(frames)
	s.ttsMu.Unlock()
	slog.InfoContext(ctx, "TTS 音频已加入 WebRTC 下行队列",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.Int("frames", len(frames)),
		slog.Bool("queued", queued),
	)
	if shouldStart {
		go s.drainTTSAudio(ctx, "tts_command")
	}
	return queued
}

func (s *Session) enqueueOrderedTTSAudio(ctx context.Context, sequence int, frames []encodedAudioFrame) bool {
	if len(frames) == 0 || sequence <= 0 {
		return false
	}
	s.ttsMu.Lock()
	s.ensureOrderedTTSLocked()
	if sequence < s.ttsOrderedNext {
		s.ttsMu.Unlock()
		slog.WarnContext(ctx, "TTS 有序音频已忽略：sequence 已过期",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.Int("sequence", sequence),
			slog.Int("nextSequence", s.ttsOrderedNext),
		)
		return false
	}
	s.ttsOrderedReady[sequence] = append(s.ttsOrderedReady[sequence], frames...)
	readyFrames := s.drainOrderedTTSLocked()
	queued := true
	shouldStart := false
	if len(readyFrames) > 0 {
		queued, shouldStart = s.appendTTSAudioLocked(readyFrames)
	}
	s.ttsMu.Unlock()
	slog.InfoContext(ctx, "TTS 有序音频已进入排序缓冲",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.Int("sequence", sequence),
		slog.Int("frames", len(frames)),
		slog.Int("releasedFrames", len(readyFrames)),
		slog.Bool("queued", queued),
	)
	if shouldStart {
		go s.drainTTSAudio(ctx, "ordered_tts")
	}
	return queued
}

func (s *Session) skipOrderedTTSAudio(ctx context.Context, sequence int, reason string) {
	s.ttsMu.Lock()
	s.ensureOrderedTTSLocked()
	if sequence < s.ttsOrderedNext {
		s.ttsMu.Unlock()
		return
	}
	s.ttsOrderedSkipped[sequence] = struct{}{}
	readyFrames := s.drainOrderedTTSLocked()
	queued := true
	shouldStart := false
	if len(readyFrames) > 0 {
		queued, shouldStart = s.appendTTSAudioLocked(readyFrames)
	}
	s.ttsMu.Unlock()
	slog.WarnContext(ctx, "TTS 有序音频序号已跳过",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.Int("sequence", sequence),
		slog.String("reason", reason),
		slog.Int("releasedFrames", len(readyFrames)),
		slog.Bool("queued", queued),
	)
	if shouldStart {
		go s.drainTTSAudio(ctx, "ordered_tts_skip")
	}
}

func (s *Session) ensureOrderedTTSLocked() {
	if s.ttsOrderedNext <= 0 {
		s.ttsOrderedNext = 1
	}
	if s.ttsOrderedReady == nil {
		s.ttsOrderedReady = map[int][]encodedAudioFrame{}
	}
	if s.ttsOrderedSkipped == nil {
		s.ttsOrderedSkipped = map[int]struct{}{}
	}
}

func (s *Session) drainOrderedTTSLocked() []encodedAudioFrame {
	s.ensureOrderedTTSLocked()
	var out []encodedAudioFrame
	for {
		if _, skipped := s.ttsOrderedSkipped[s.ttsOrderedNext]; skipped {
			delete(s.ttsOrderedSkipped, s.ttsOrderedNext)
			s.ttsOrderedNext++
			continue
		}
		frames, ok := s.ttsOrderedReady[s.ttsOrderedNext]
		if !ok {
			return out
		}
		out = append(out, frames...)
		delete(s.ttsOrderedReady, s.ttsOrderedNext)
		s.ttsOrderedNext++
	}
}

func (s *Session) appendTTSAudioLocked(frames []encodedAudioFrame) (bool, bool) {
	s.ttsQueue = append(s.ttsQueue, frames...)
	connected := s.peer != nil && s.peer.ConnectionState() == pionwebrtc.PeerConnectionStateConnected
	queued := !connected
	shouldStart := connected && !s.ttsPlaying
	if shouldStart {
		s.ttsPlaying = true
	}
	return queued, shouldStart
}

// startTTSDrain 在 WebRTC 连接建立后启动已排队 TTS 音频的发送循环。
func (s *Session) startTTSDrain(ctx context.Context, reason string) {
	s.ttsMu.Lock()
	if s.ttsPlaying || len(s.ttsQueue) == 0 {
		s.ttsMu.Unlock()
		return
	}
	s.ttsPlaying = true
	s.ttsMu.Unlock()
	go s.drainTTSAudio(ctx, reason)
}

// drainTTSAudio 按实时节奏把 PCMU 帧写入 WebRTC 下行音频轨道。
// 逻辑: 循环从队列取一帧，写入 TrackLocalStaticSample，按帧时长 sleep，队列耗尽后退出。
func (s *Session) drainTTSAudio(ctx context.Context, reason string) {
	started := time.Now()
	var sentFrames int
	var sentBytes int
	defer func() {
		slog.InfoContext(ctx, "TTS WebRTC 下行播放循环结束",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("reason", reason),
			slog.Int("frames", sentFrames),
			slog.Int("audioBytes", sentBytes),
			slog.Duration("duration", time.Since(started)),
		)
	}()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		s.ttsMu.Lock()
		if len(s.ttsQueue) == 0 {
			s.ttsPlaying = false
			s.ttsMu.Unlock()
			return
		}
		frame := s.ttsQueue[0]
		copy(s.ttsQueue, s.ttsQueue[1:])
		s.ttsQueue = s.ttsQueue[:len(s.ttsQueue)-1]
		s.ttsMu.Unlock()

		if s.ttsTrack == nil {
			slog.WarnContext(ctx, "TTS WebRTC 下行轨道不存在",
				slog.String("connectionId", s.connectionID),
				slog.String("callId", s.callID),
			)
			continue
		}
		if err := s.ttsTrack.WriteSample(pionmedia.Sample{Data: frame.payload, Duration: frame.duration}); err != nil {
			slog.WarnContext(ctx, "TTS WebRTC 音频帧发送失败",
				slog.String("connectionId", s.connectionID),
				slog.String("callId", s.callID),
				slog.Any("error", err),
			)
			return
		}
		sentFrames++
		sentBytes += len(frame.payload)
		select {
		case <-ctx.Done():
			return
		case <-time.After(frame.duration):
		}
	}
}

// encodePlaybackChunk 将 TTS provider 输出的 wav/pcm 音频转换为 WebRTC PCMU/8000 帧。
func encodePlaybackChunk(chunk PlaybackChunk) ([]encodedAudioFrame, error) {
	format := strings.ToLower(strings.TrimSpace(chunk.Format))
	if format == "" {
		format = "pcm"
	}
	var samples []int16
	sampleRate := chunk.SampleRate
	var err error
	switch format {
	case "wav", "wave":
		samples, sampleRate, err = decodeWAVPCM16(chunk.Payload)
	case "pcm", "raw", "s16le":
		if sampleRate == 0 {
			sampleRate = 16000
		}
		samples, err = pcm16LEToMonoSamples(chunk.Payload, 1)
	case "mp3":
		return nil, errors.New("tts mp3 cannot be injected into webrtc without decoder; use wav or pcm")
	default:
		return nil, fmt.Errorf("unsupported tts audio format for webrtc: %s", format)
	}
	if err != nil {
		return nil, err
	}
	samples = resamplePCM16Nearest(samples, sampleRate, 8000)
	if len(samples) == 0 {
		return nil, errors.New("tts audio has no pcm samples")
	}
	pcmu := make([]byte, len(samples))
	for index, sample := range samples {
		pcmu[index] = linearPCM16ToMuLaw(sample)
	}
	return splitPCMUFrames(pcmu), nil
}

// decodeWAVPCM16 解析 RIFF/WAVE PCM16 音频，并返回单声道采样。
func decodeWAVPCM16(payload []byte) ([]int16, int, error) {
	if len(payload) < 44 || string(payload[0:4]) != "RIFF" || string(payload[8:12]) != "WAVE" {
		return nil, 0, errors.New("invalid wav tts audio")
	}
	reader := bytes.NewReader(payload[12:])
	var sampleRate int
	var channels int
	var data []byte
	for reader.Len() >= 8 {
		var id [4]byte
		if _, err := reader.Read(id[:]); err != nil {
			return nil, 0, err
		}
		var size uint32
		if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
			return nil, 0, err
		}
		if uint32(reader.Len()) < size {
			return nil, 0, errors.New("truncated wav chunk")
		}
		chunk := make([]byte, size)
		if _, err := reader.Read(chunk); err != nil {
			return nil, 0, err
		}
		if size%2 == 1 && reader.Len() > 0 {
			_, _ = reader.ReadByte()
		}
		switch string(id[:]) {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, errors.New("invalid wav fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(chunk[0:2])
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSample := binary.LittleEndian.Uint16(chunk[14:16])
			if audioFormat != 1 || bitsPerSample != 16 {
				return nil, 0, errors.New("only wav pcm16 tts audio is supported")
			}
		case "data":
			data = chunk
		}
	}
	if sampleRate == 0 || channels == 0 || len(data) == 0 {
		return nil, 0, errors.New("wav tts audio missing fmt or data")
	}
	samples, err := pcm16LEToMonoSamples(data, channels)
	return samples, sampleRate, err
}

// pcm16LEToMonoSamples 将 PCM16LE 字节转成单声道采样，多声道时取每帧第一个声道。
func pcm16LEToMonoSamples(payload []byte, channels int) ([]int16, error) {
	if channels <= 0 {
		return nil, errors.New("pcm channel count must be positive")
	}
	frameBytes := channels * 2
	if len(payload) < frameBytes {
		return nil, errors.New("pcm tts audio is too short")
	}
	frameCount := len(payload) / frameBytes
	samples := make([]int16, 0, frameCount)
	for offset := 0; offset+frameBytes <= len(payload); offset += frameBytes {
		samples = append(samples, int16(binary.LittleEndian.Uint16(payload[offset:offset+2])))
	}
	return samples, nil
}

// resamplePCM16Nearest 用最近邻采样把 PCM16 转到目标采样率，满足 PCMU/8000 的 WebRTC 发送要求。
func resamplePCM16Nearest(samples []int16, sourceRate, targetRate int) []int16 {
	if sourceRate <= 0 || targetRate <= 0 || len(samples) == 0 {
		return nil
	}
	if sourceRate == targetRate {
		return samples
	}
	outLen := int(float64(len(samples)) * float64(targetRate) / float64(sourceRate))
	if outLen <= 0 {
		outLen = 1
	}
	out := make([]int16, outLen)
	for index := range out {
		sourceIndex := int(float64(index) * float64(sourceRate) / float64(targetRate))
		if sourceIndex >= len(samples) {
			sourceIndex = len(samples) - 1
		}
		out[index] = samples[sourceIndex]
	}
	return out
}

// linearPCM16ToMuLaw 把线性 PCM16 采样编码为 G.711 mu-law 字节。
func linearPCM16ToMuLaw(sample int16) byte {
	const bias = 0x84
	const clip = 32635
	pcm := int(sample)
	sign := 0
	if pcm < 0 {
		sign = 0x80
		pcm = -pcm
	}
	if pcm > clip {
		pcm = clip
	}
	pcm += bias
	exponent := 7
	for mask := 0x4000; exponent > 0 && pcm&mask == 0; exponent-- {
		mask >>= 1
	}
	mantissa := (pcm >> (exponent + 3)) & 0x0f
	return ^byte(sign | exponent<<4 | mantissa)
}

// splitPCMUFrames 将 PCMU 字节切成 20ms 一帧，交给 Pion sample track packetize。
func splitPCMUFrames(payload []byte) []encodedAudioFrame {
	const samplesPerFrame = 160
	frames := make([]encodedAudioFrame, 0, (len(payload)+samplesPerFrame-1)/samplesPerFrame)
	for offset := 0; offset < len(payload); offset += samplesPerFrame {
		end := offset + samplesPerFrame
		if end > len(payload) {
			end = len(payload)
		}
		duration := time.Duration(end-offset) * time.Second / 8000
		if duration <= 0 {
			duration = 20 * time.Millisecond
		}
		frames = append(frames, encodedAudioFrame{payload: append([]byte(nil), payload[offset:end]...), duration: duration})
	}
	return frames
}

// logICEFailure 打印 ICE 失败时的候选和 pair 摘要，帮助定位地址/端口是否可达。
func logICEFailure(ctx context.Context, session *Session) {
	candidates := session.candidateSnapshot()
	slog.WarnContext(ctx, "WebRTC ICE 连接失败，未选出可用候选对",
		slog.String("connectionId", session.connectionID),
		slog.String("callId", session.callID),
		slog.String("userId", session.userID),
		slog.Duration("duration", time.Since(session.createdAt)),
		slog.Any("cachedLocalCandidates", candidates.Local),
		slog.Any("cachedRemoteCandidates", candidates.Remote),
		slog.Any("iceStats", summarizeICEStats(session.peer.GetStats())),
	)
}

type iceCandidateSnapshot struct {
	Local  []iceCandidateSummary
	Remote []iceCandidateSummary
}

// recordLocalICECandidate 缓存后端本地 ICE candidate 摘要，便于 failed 状态下排查候选收集情况。
func (s *Session) recordLocalICECandidate(raw string) iceCandidateSummary {
	return s.recordICECandidate(raw, true)
}

// recordRemoteICECandidate 缓存浏览器远端 ICE candidate 摘要，便于确认前端是否把候选传回后端。
func (s *Session) recordRemoteICECandidate(raw string) iceCandidateSummary {
	return s.recordICECandidate(raw, false)
}

// recordICECandidate 记录 ICE candidate 摘要。
// 逻辑: 先把原始 candidate 转成结构化摘要，再加锁写入本地或远端候选列表，最后返回同一份摘要供日志复用。
func (s *Session) recordICECandidate(raw string, local bool) iceCandidateSummary {
	summary := summarizeICECandidate(raw)
	s.candidateMu.Lock()
	defer s.candidateMu.Unlock()
	if local {
		s.localCandidates = append(s.localCandidates, summary)
		return summary
	}
	s.remoteCandidates = append(s.remoteCandidates, summary)
	return summary
}

// candidateSnapshot 返回当前会话已缓存的本地和远端 ICE candidate 副本。
func (s *Session) candidateSnapshot() iceCandidateSnapshot {
	s.candidateMu.Lock()
	defer s.candidateMu.Unlock()
	return iceCandidateSnapshot{
		Local:  append([]iceCandidateSummary(nil), s.localCandidates...),
		Remote: append([]iceCandidateSummary(nil), s.remoteCandidates...),
	}
}

// queueASRFrame 将一帧 WebRTC 上行音频写入当前 ASR 实时流。
// 逻辑:
// 1. 先按 provider 需要转换音频格式，腾讯 ASR 会把 WebRTC Opus 转成 PCM16LE/16k。
// 2. 本地 VAD 门控会在语音开始前丢弃静音，并在连续静音后关闭当前 ASR 流形成一句话。
// 3. 首个有效语音帧懒加载创建 ASR 长连接，后续帧复用同一连接连续写入。
func (s *Session) queueASRFrame(ctx context.Context, payload []byte, codec string) {
	if len(payload) == 0 {
		return
	}
	s.queueRecordingFrame(ctx, payload, codec)
	if s.asrClient == nil {
		reason := s.asrDisabledReason
		if reason == "" {
			reason = "asr_not_enabled"
		}
		s.logASRSkippedPayload(ctx, codec, len(payload), reason)
		return
	}
	inputBytes := len(payload)
	payload, ok := s.prepareASRPayload(ctx, payload, codec)
	if !ok {
		return
	}
	s.logASRPreparedPayload(ctx, codec, inputBytes, len(payload))
	if s.asrGate != nil && s.asrGate.enabled {
		decision := s.asrGate.Evaluate(payload)
		s.logASRVoiceGateDecision(ctx, decision)
		for _, framePayload := range decision.WriteFrames {
			s.writeASRPayload(ctx, framePayload)
		}
		if decision.SpeechEnd {
			s.finishASRUtterance(ctx, "vad_speech_end")
		}
		return
	}
	s.writeASRPayload(ctx, payload)
}

func (s *Session) queueRecordingFrame(ctx context.Context, payload []byte, codec string) {
	s.recordingMu.Lock()
	recorder := s.recording
	processor := s.recordingProcessor
	s.recordingMu.Unlock()
	if recorder == nil {
		return
	}
	recordingPayload := payload
	if processor != nil && strings.Contains(strings.ToLower(codec), "opus") {
		out, err := processor.decodeOpusToPCM16LE(payload)
		if err != nil {
			slog.WarnContext(ctx, "WebRTC 录音 Opus 音频解码失败，已跳过该包",
				slog.String("connectionId", s.connectionID),
				slog.String("callId", s.callID),
				slog.String("userId", s.userID),
				slog.String("codec", codec),
				slog.Int("packetBytes", len(payload)),
				slog.Any("error", err),
			)
			return
		}
		recordingPayload = out
	}
	if len(recordingPayload) == 0 {
		return
	}
	if ok := recorder.WritePCM16LE(recordingPayload); !ok {
		dropped := recorder.DroppedFrames()
		if dropped == 1 || dropped%audioLogEveryPackets == 0 {
			slog.WarnContext(ctx, "WebRTC 录音缓冲已满，音频帧已丢弃",
				slog.String("connectionId", s.connectionID),
				slog.String("callId", s.callID),
				slog.String("userId", s.userID),
				slog.Int64("droppedFrames", dropped),
			)
		}
	}
}

func (s *Session) logASRSkippedPayload(ctx context.Context, codec string, payloadBytes int, reason string) {
	frameCount := s.asrSkippedFrames.Add(1)
	totalBytes := s.asrSkippedBytes.Add(int64(payloadBytes))
	if frameCount != 1 && frameCount%audioLogEveryPackets != 0 {
		return
	}
	slog.WarnContext(ctx, "WebRTC ASR 未启用，音频帧未进入 VAD/ASR",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("reason", reason),
		slog.String("inputCodec", codec),
		slog.Int64("skippedFrames", frameCount),
		slog.Int64("skippedBytes", totalBytes),
		slog.Int("frameBytes", payloadBytes),
	)
}

func (s *Session) logASRPreparedPayload(ctx context.Context, codec string, inputBytes, outputBytes int) {
	frameCount := s.asrPreparedFrames.Add(1)
	// totalBytes := s.asrPreparedBytes.Add(int64(outputBytes))
	if frameCount != 1 && frameCount%audioLogEveryPackets != 0 {
		return
	}
	// slog.InfoContext(ctx, "WebRTC ASR 音频已准备进入 VAD/ASR",
	// 	slog.String("connectionId", s.connectionID),
	// 	slog.String("callId", s.callID),
	// 	slog.String("userId", s.userID),
	// 	slog.String("provider", s.asrClient.Provider()),
	// 	slog.String("inputCodec", codec),
	// 	slog.Int64("preparedFrames", frameCount),
	// 	slog.Int64("preparedBytes", totalBytes),
	// 	slog.Int("inputBytes", inputBytes),
	// 	slog.Int("outputBytes", outputBytes),
	// 	slog.Bool("converted", s.asrProcessor != nil),
	// )
}

// writeASRPayload 把一帧已转换后的音频写入 ASR 实时流。
func (s *Session) writeASRPayload(ctx context.Context, payload []byte) {
	if len(payload) == 0 {
		return
	}
	stream, ok := s.ensureASRStream(ctx)
	if !ok {
		return
	}
	frame := asr.Frame{CallID: s.callID, Payload: payload}
	if err := stream.Write(ctx, frame); err != nil {
		s.disableASRStream(ctx, stream, err)
		return
	}
	// frameCount := s.asrWrittenFrames.Add(1)
	// totalBytes := s.asrWrittenBytes.Add(int64(len(payload)))
	// if frameCount == 1 || frameCount%audioLogEveryPackets == 0 {
	// 	slog.InfoContext(ctx, "WebRTC ASR 音频帧已写入 provider",
	// 		slog.String("connectionId", s.connectionID),
	// 		slog.String("callId", s.callID),
	// 		slog.String("userId", s.userID),
	// 		slog.String("provider", s.asrClient.Provider()),
	// 		slog.Int64("writtenFrames", frameCount),
	// 		slog.Int64("writtenBytes", totalBytes),
	// 		slog.Int("frameBytes", len(payload)),
	// 	)
	// }
}

// prepareASRPayload 按 ASR provider 需求转换 WebRTC 音频；腾讯 ASR 使用 PCM16LE/16k，其他 provider 原样透传。
func (s *Session) prepareASRPayload(ctx context.Context, payload []byte, codec string) ([]byte, bool) {
	if s.asrProcessor == nil {
		return append([]byte(nil), payload...), true
	}
	out, err := s.asrProcessor.decodeOpusToPCM16LE(payload)
	if err != nil {
		slog.WarnContext(ctx, "WebRTC Opus 音频解码失败，已跳过该包",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("codec", codec),
			slog.Int("packetBytes", len(payload)),
			slog.Any("error", err),
		)
		return nil, false
	}
	return out, len(out) > 0
}

// logASRVoiceGateDecision 打印本地 VAD 门控的关键决策，方便确认静音是否被过滤、语音是否切句。
func (s *Session) logASRVoiceGateDecision(ctx context.Context, decision asrVoiceGateDecision) {
	if decision.SpeechStart {
		slog.InfoContext(ctx, "WebRTC 本地 VAD 检测到语音开始",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("vadSource", decision.VADSource),
			slog.Float64("vadConfidence", decision.VADConfidence),
			slog.Float64("vadThreshold", decision.VADThreshold),
			slog.Float64("energy", decision.Energy),
			slog.Int("preRollFrames", len(decision.WriteFrames)),
		)
		return
	}
	if decision.SpeechEnd {
		slog.InfoContext(ctx, "WebRTC 本地 VAD 检测到语音结束",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("vadSource", decision.VADSource),
			slog.Float64("vadConfidence", decision.VADConfidence),
			slog.Float64("vadThreshold", decision.VADThreshold),
			slog.Float64("energy", decision.Energy),
			slog.Int("droppedFrames", decision.DroppedFrames),
			slog.Int("droppedBytes", decision.DroppedBytes),
		)
		return
	}
	if decision.DroppedFrames > 0 && decision.DroppedFrames%audioLogEveryPackets == 0 {
		slog.InfoContext(ctx, "WebRTC 本地 VAD 正在丢弃静音音频",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("vadSource", decision.VADSource),
			slog.Float64("vadConfidence", decision.VADConfidence),
			slog.Float64("vadThreshold", decision.VADThreshold),
			slog.Float64("energy", decision.Energy),
			slog.Int("droppedFrames", decision.DroppedFrames),
			slog.Int("droppedBytes", decision.DroppedBytes),
		)
	}
}

// ensureASRStream 返回当前会话 ASR 实时流，必要时创建一次新流。
// 逻辑:
// 1. 已禁用或 provider 不支持实时流时直接返回 false。
// 2. 当前没有流时调用 asr.Client.OpenStream 建立长连接，并启动结果消费 goroutine。
// 3. 建立失败后禁用本次会话 ASR，避免每个音频包都重复重连。
func (s *Session) ensureASRStream(ctx context.Context) (asr.Stream, bool) {
	s.asrMu.Lock()
	if s.asrStreamDisabled {
		s.asrMu.Unlock()
		return nil, false
	}
	if s.asrStream != nil {
		stream := s.asrStream
		s.asrMu.Unlock()
		return stream, true
	}
	s.asrMu.Unlock()

	if !s.asrClient.SupportsStreaming() {
		s.asrMu.Lock()
		s.asrStreamDisabled = true
		s.asrMu.Unlock()
		slog.WarnContext(ctx, "WebRTC ASR provider 不支持实时流，已禁用本次会话 ASR",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("provider", s.asrClient.Provider()),
		)
		return nil, false
	}
	stream, err := s.asrClient.OpenStream(ctx, s.callID)
	if err != nil {
		s.asrMu.Lock()
		s.asrStreamDisabled = true
		s.asrMu.Unlock()
		slog.ErrorContext(ctx, "WebRTC ASR 实时流创建失败，已禁用本次会话 ASR",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("provider", s.asrClient.Provider()),
			slog.Any("error", err),
		)
		return nil, false
	}
	s.asrMu.Lock()
	if s.asrStreamDisabled {
		s.asrMu.Unlock()
		_ = stream.Close(ctx)
		return nil, false
	}
	if s.asrStream != nil {
		existing := s.asrStream
		s.asrMu.Unlock()
		_ = stream.Close(ctx)
		return existing, true
	}
	s.asrStream = stream
	utteranceID := s.nextUtteranceIDLocked()
	s.asrMu.Unlock()
	s.resetASRUtteranceState()
	s.resetTMTUtteranceState()
	go s.consumeASRStream(ctx, stream, utteranceID)
	slog.InfoContext(ctx, "WebRTC ASR 实时流已建立",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("provider", s.asrClient.Provider()),
		slog.String("utteranceId", utteranceID),
	)
	return stream, true
}

func (s *Session) nextUtteranceIDLocked() string {
	s.utteranceSeq++
	callID := s.callID
	if callID == "" {
		callID = "call"
	}
	return fmt.Sprintf("%s-utt-%d", callID, s.utteranceSeq)
}

func (s *Session) beginASRUtterance() string {
	s.asrMu.Lock()
	utteranceID := s.nextUtteranceIDLocked()
	s.asrMu.Unlock()
	s.resetASRUtteranceState()
	s.resetTMTUtteranceState()
	return utteranceID
}

// consumeASRStream 持续消费 ASR 实时流结果和错误，并转发给 WebSocket 回调。
func (s *Session) consumeASRStream(ctx context.Context, stream asr.Stream, utteranceID string) {
	resultCh := stream.Results()
	errorCh := stream.Errors()
	defer s.clearASRStream(stream)
	for resultCh != nil || errorCh != nil {
		select {
		case result, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			if utteranceID == "" && strings.TrimSpace(result.Text) != "" {
				utteranceID = s.beginASRUtterance()
				slog.InfoContext(ctx, "WebRTC ASR 新分句已分配",
					slog.String("connectionId", s.connectionID),
					slog.String("callId", s.callID),
					slog.String("userId", s.userID),
					slog.String("utteranceId", utteranceID),
				)
			}
			if s.forwardASRResult(ctx, result, utteranceID) && result.IsFinal {
				utteranceID = ""
			}
		case err, ok := <-errorCh:
			if !ok {
				errorCh = nil
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				slog.InfoContext(ctx, "WebRTC ASR 实时流已取消",
					slog.String("connectionId", s.connectionID),
					slog.String("callId", s.callID),
					slog.String("userId", s.userID),
					slog.String("provider", s.asrClient.Provider()),
					slog.Any("error", err),
				)
				continue
			}
			s.disableASRStream(ctx, stream, err)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Session) resetTMTUtteranceState() {
	s.tmtMu.Lock()
	defer s.tmtMu.Unlock()
	s.tmtForwarded = map[string]struct{}{}
}

func (s *Session) resetASRUtteranceState() {
	s.asrResultMu.Lock()
	defer s.asrResultMu.Unlock()
	s.asrForwarded = map[string]struct{}{}
}

// forwardASRResult 将 ASR provider 结果转换为 WebSocket 层使用的模型并回调。
// 逻辑:
// 1. 默认丢弃实时 ASR partial，避免腾讯中间假设不断修正时对业务服务重复回调。
// 2. 对文本和 final 状态做会话内去重，防止 provider 重放同一结果。
// 3. 只有通过收敛检查的结果才生成 utteranceID 并交给 WebSocket 层。
func (s *Session) forwardASRResult(ctx context.Context, result asr.Result, utteranceID string) bool {
	if s.onASRResult == nil && s.eventBus == nil {
		return false
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" {
		return false
	}
	if !result.IsFinal && !s.asrForwardPartial {
		slog.DebugContext(ctx, "WebRTC ASR partial 已过滤，等待最终识别结果",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.Int("textRunes", len([]rune(result.Text))),
		)
		return false
	}
	if !s.markASRResultForwarded(result) {
		slog.DebugContext(ctx, "WebRTC ASR 重复结果已过滤",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.Bool("isFinal", result.IsFinal),
			slog.Int("textRunes", len([]rune(result.Text))),
		)
		return false
	}
	asrResult := model.ASRResult{
		CallID:      result.CallID,
		UtteranceID: utteranceID,
		Text:        result.Text,
		IsFinal:     result.IsFinal,
		Confidence:  result.Confidence,
		Language:    firstNonEmpty(s.sourceLanguage, model.DefaultSourceLanguage),
	}
	slog.InfoContext(ctx, "WebRTC ASR 结果已转发到业务回调",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("utteranceId", utteranceID),
		slog.Bool("isFinal", result.IsFinal),
		slog.Float64("confidence", result.Confidence),
		slog.Int("textRunes", len([]rune(result.Text))),
		slog.String("textPreview", previewASRText(result.Text, 80)),
	)
	if s.onASRResult != nil {
		s.onASRResult(asrResult)
	}
	asrEvent := pbxevents.NewASRResultEvent(pbxevents.ASRResultPayload{
		ConnectionID: s.connectionID,
		CallID:       asrResult.CallID,
		UserID:       s.userID,
		UtteranceID:  asrResult.UtteranceID,
		Text:         asrResult.Text,
		IsFinal:      asrResult.IsFinal,
		Confidence:   asrResult.Confidence,
		Language:     asrResult.Language,
		Provider:     s.asrProvider,
		Metadata: compactMetadata(map[string]string{
			"utteranceId": asrResult.UtteranceID,
			"provider":    s.asrProvider,
		}),
	})
	if asrResult.IsFinal {
		s.publishRequiredEvent(ctx, asrEvent)
	} else {
		s.publishBestEffortEvent(ctx, asrEvent)
	}
	s.queueTMTTranslation(ctx, asrResult)
	return true
}

// markASRResultForwarded 记录已转发 ASR 结果，返回 false 表示重复结果应被过滤。
func (s *Session) markASRResultForwarded(result asr.Result) bool {
	key := asrResultForwardKey(result)
	s.asrResultMu.Lock()
	defer s.asrResultMu.Unlock()
	if s.asrForwarded == nil {
		s.asrForwarded = map[string]struct{}{}
	}
	if _, exists := s.asrForwarded[key]; exists {
		return false
	}
	s.asrForwarded[key] = struct{}{}
	return true
}

func (s *Session) queueTMTTranslation(ctx context.Context, result model.ASRResult) {
	if s.tmtClient == nil || (s.onTranslation == nil && s.eventBus == nil) {
		return
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" || result.UtteranceID == "" {
		return
	}
	if !result.IsFinal {
		slog.DebugContext(ctx, "WebRTC ASR partial 不触发 TMT，等待最终识别结果",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("utteranceId", result.UtteranceID),
			slog.Int("textRunes", len([]rune(result.Text))),
		)
		return
	}
	if !s.reserveTMTTranslation(result) {
		return
	}
	go s.runTMTTranslation(ctx, result)
}

func (s *Session) reserveTMTTranslation(result model.ASRResult) bool {
	if !result.IsFinal {
		return false
	}
	normalized := strings.Join(strings.Fields(result.Text), " ")
	if normalized == "" {
		return false
	}
	key := "final:" + result.UtteranceID + ":" + normalized
	s.tmtMu.Lock()
	defer s.tmtMu.Unlock()
	if s.tmtForwarded == nil {
		s.tmtForwarded = map[string]struct{}{}
	}
	if _, exists := s.tmtForwarded[key]; exists {
		return false
	}
	s.tmtForwarded[key] = struct{}{}
	return true
}

func (s *Session) runTMTTranslation(ctx context.Context, result model.ASRResult) {
	started := time.Now()
	translated, err := s.tmtClient.Translate(ctx, tmt.Request{
		CallID:      result.CallID,
		UtteranceID: result.UtteranceID,
		Text:        result.Text,
		SourceLang:  firstNonEmpty(s.sourceLanguage, model.DefaultSourceLanguage),
		TargetLang:  firstNonEmpty(s.targetLanguage, model.DefaultTargetLanguage),
		Quality:     result.IsFinal,
	})
	if err != nil {
		slog.WarnContext(ctx, "WebRTC TMT 翻译失败，保留英文 ASR 主链路",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", result.CallID),
			slog.String("userId", s.userID),
			slog.String("utteranceId", result.UtteranceID),
			slog.Bool("isFinal", result.IsFinal),
			slog.String("provider", s.tmtClient.Provider()),
			slog.Duration("elapsed", time.Since(started)),
			slog.Any("error", err),
		)
		s.publishRequiredEvent(ctx, pbxevents.NewErrorEvent(pbxevents.ErrorPayload{
			ConnectionID: s.connectionID,
			CallID:       result.CallID,
			UserID:       s.userID,
			Error:        err.Error(),
			Metadata: compactMetadata(map[string]string{
				"source":      "tmt",
				"provider":    s.tmtProvider,
				"utteranceId": result.UtteranceID,
			}),
		}))
		return
	}
	translation := strings.TrimSpace(translated.Text)
	if translation == "" {
		return
	}
	out := model.TranslationResult{
		CallID:      result.CallID,
		UtteranceID: result.UtteranceID,
		SourceText:  result.Text,
		Text:        translation,
		IsFinal:     result.IsFinal,
		Engine:      "tmt",
		Revised:     false,
		Language:    firstNonEmpty(s.targetLanguage, model.DefaultTargetLanguage),
	}
	slog.InfoContext(ctx, "WebRTC TMT 翻译结果已转发到业务回调",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", result.CallID),
		slog.String("userId", s.userID),
		slog.String("utteranceId", result.UtteranceID),
		slog.Bool("isFinal", result.IsFinal),
		slog.String("provider", s.tmtClient.Provider()),
		slog.Int("sourceRunes", len([]rune(result.Text))),
		slog.Int("textRunes", len([]rune(translation))),
		slog.String("textPreview", previewASRText(translation, 80)),
		slog.Duration("elapsed", time.Since(started)),
	)
	if s.onTranslation != nil {
		s.onTranslation(out)
	}
	s.publishRequiredEvent(ctx, pbxevents.NewTranslationResultEvent(pbxevents.TranslationResultPayload{
		ConnectionID: s.connectionID,
		CallID:       out.CallID,
		UserID:       s.userID,
		UtteranceID:  out.UtteranceID,
		SourceText:   out.SourceText,
		Text:         out.Text,
		IsFinal:      out.IsFinal,
		Engine:       out.Engine,
		Revised:      out.Revised,
		Language:     out.Language,
		Provider:     s.tmtProvider,
		Metadata: compactMetadata(map[string]string{
			"utteranceId": out.UtteranceID,
			"engine":      out.Engine,
			"provider":    s.tmtProvider,
		}),
	}))
}

// disableASRStream 记录 ASR 实时流错误并禁用本次会话后续 ASR 写入。
func (s *Session) disableASRStream(ctx context.Context, stream asr.Stream, err error) {
	s.asrMu.Lock()
	if s.asrStream == stream {
		s.asrStream = nil
	}
	s.asrStreamDisabled = true
	s.asrMu.Unlock()
	_ = stream.Close(ctx)
	slog.ErrorContext(ctx, "WebRTC ASR 实时流失败，已禁用本次会话 ASR",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("provider", s.asrClient.Provider()),
		slog.Any("error", err),
	)
	s.publishRequiredEvent(ctx, pbxevents.NewErrorEvent(pbxevents.ErrorPayload{
		ConnectionID: s.connectionID,
		CallID:       s.callID,
		UserID:       s.userID,
		Error:        err.Error(),
		Metadata: compactMetadata(map[string]string{
			"source":   "asr_stream",
			"provider": s.asrProvider,
		}),
	}))
}

// clearASRStream 在流自然结束后清理会话中保存的流引用。
func (s *Session) clearASRStream(stream asr.Stream) {
	s.asrMu.Lock()
	defer s.asrMu.Unlock()
	if s.asrStream == stream {
		s.asrStream = nil
	}
}

// closeASRStream 结束本次 WebRTC 会话的 ASR 实时流。
func (s *Session) closeASRStream(ctx context.Context, reason string) {
	s.asrMu.Lock()
	stream := s.asrStream
	s.asrStream = nil
	s.asrStreamDisabled = true
	s.asrMu.Unlock()
	if stream == nil {
		return
	}
	if err := stream.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "WebRTC ASR 实时流关闭异常",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("reason", reason),
			slog.String("provider", s.asrClient.Provider()),
			slog.Any("error", err),
		)
		return
	}
	slog.InfoContext(ctx, "WebRTC ASR 实时流已关闭",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("reason", reason),
		slog.String("provider", s.asrClient.Provider()),
	)
}

func (s *Session) closeRecording(ctx context.Context, reason string) {
	s.recordingMu.Lock()
	recorder := s.recording
	s.recording = nil
	s.recordingProcessor = nil
	s.recordingMu.Unlock()
	if recorder == nil {
		return
	}
	uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	metadata, err := recorder.Close(uploadCtx)
	if err != nil {
		slog.WarnContext(ctx, "WebRTC 录音停止失败",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("reason", reason),
			slog.Any("error", err),
		)
		s.publishRequiredEvent(ctx, pbxevents.NewErrorEvent(pbxevents.ErrorPayload{
			ConnectionID: s.connectionID,
			CallID:       s.callID,
			UserID:       s.userID,
			Error:        err.Error(),
			Metadata: compactMetadata(map[string]string{
				"source": "recording",
				"reason": reason,
			}),
		}))
		return
	}
	dropped := recorder.DroppedFrames()
	slog.InfoContext(ctx, "WebRTC 录音已完成",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("reason", reason),
		slog.String("recordingId", metadata.ID),
		slog.String("objectKey", metadata.ObjectKey),
		slog.Int64("size", metadata.Size),
		slog.Int64("droppedFrames", dropped),
	)
	s.publishRequiredEvent(ctx, pbxevents.NewRecordingResultEvent(pbxevents.RecordingResultPayload{
		ConnectionID: s.connectionID,
		CallID:       s.callID,
		UserID:       s.userID,
		RecordingID:  metadata.ID,
		ObjectKey:    metadata.ObjectKey,
		Checksum:     metadata.Checksum,
		Size:         metadata.Size,
		SampleRate:   s.recordingSampleRate,
		Format:       "wav",
		StartedAt:    metadata.StartedAt.UTC().Format(time.RFC3339Nano),
		StoppedAt:    metadata.StoppedAt.UTC().Format(time.RFC3339Nano),
		Metadata: compactMetadata(map[string]string{
			"reason":        reason,
			"droppedFrames": strconv.FormatInt(dropped, 10),
		}),
	}))
}

// finishASRUtterance 结束当前一句话的 ASR 流，但保留会话 ASR 能力，后续语音可重新建流。
func (s *Session) finishASRUtterance(ctx context.Context, reason string) {
	s.asrMu.Lock()
	stream := s.asrStream
	s.asrStream = nil
	s.asrMu.Unlock()
	if stream == nil {
		return
	}
	go s.closeASRUtteranceStream(ctx, stream, reason)
}

// closeASRUtteranceStream 异步关闭一句话的 ASR 流，避免阻塞 WebRTC RTP 读取。
func (s *Session) closeASRUtteranceStream(ctx context.Context, stream asr.Stream, reason string) {
	if err := stream.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "WebRTC ASR 语音段关闭异常",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("userId", s.userID),
			slog.String("reason", reason),
			slog.String("provider", s.asrClient.Provider()),
			slog.Any("error", err),
		)
		return
	}
	slog.InfoContext(ctx, "WebRTC ASR 语音段已关闭，等待后续语音重新建流",
		slog.String("connectionId", s.connectionID),
		slog.String("callId", s.callID),
		slog.String("userId", s.userID),
		slog.String("reason", reason),
		slog.String("provider", s.asrClient.Provider()),
	)
}

// Evaluate 根据 PCM16LE RMS 能量更新本地 VAD 状态，并返回本帧是否需要写入 ASR。
// 逻辑:
// 1. 未说话时维护预滚 buffer，只在连续语音达到阈值后把预滚帧一起放行。
// 2. 说话中持续放行语音和尾部静音，保证 ASR 能拿到自然结尾。
// 3. 连续静音达到阈值后标记语音结束，由调用方关闭当前 ASR 流。
func (g *asrVoiceGate) Evaluate(payload []byte) asrVoiceGateDecision {
	if g == nil || !g.enabled {
		energy := pcm16RMSNormalized(payload)
		return asrVoiceGateDecision{WriteFrames: [][]byte{append([]byte(nil), payload...)}, Speech: true, Energy: energy, VADConfidence: energy, VADSource: "disabled"}
	}
	if g.detector != nil && g.vadSessionID != "" {
		return g.evaluateWithDetector(payload)
	}
	return g.evaluateByEnergy(payload)
}

// evaluateByEnergy 使用本地 PCM16 RMS 能量门控，是默认和 VAD provider 失败时的回退路径。
func (g *asrVoiceGate) evaluateByEnergy(payload []byte) asrVoiceGateDecision {
	energy := pcm16RMSNormalized(payload)
	return g.evaluateSpeech(payload, energy >= g.threshold, energy, energy, g.threshold, "rms")
}

// evaluateWithDetector 使用注入的 VAD detector 置信度驱动 ASR 开流、放行和切句。
func (g *asrVoiceGate) evaluateWithDetector(payload []byte) asrVoiceGateDecision {
	score, err := g.detector.Score(vad.Frame{CallID: g.vadSessionID, Payload: payload})
	if err != nil {
		slog.Warn("WebRTC VAD detector 推理失败，回退本地 RMS 门控",
			slog.String("vadSessionId", g.vadSessionID),
			slog.Any("error", err),
		)
		return g.evaluateByEnergy(payload)
	}
	threshold := g.detectorThreshold
	if threshold <= 0 {
		threshold = 0.5
	}
	if !score.Scored {
		return g.evaluateDetectorPending(payload, score.Energy, score.Confidence, threshold)
	}
	return g.evaluateSpeech(payload, score.Confidence >= threshold, score.Energy, score.Confidence, threshold, "detector")
}

func (g *asrVoiceGate) evaluateDetectorPending(payload []byte, energy, confidence, threshold float64) asrVoiceGateDecision {
	base := asrVoiceGateDecision{Energy: energy, VADConfidence: confidence, VADThreshold: threshold, VADSource: "detector"}
	if !g.speaking {
		g.pushPreRoll(payload)
		return base
	}
	base.WriteFrames = [][]byte{append([]byte(nil), payload...)}
	base.Speech = true
	base.DroppedFrames = g.droppedFrames
	base.DroppedBytes = g.droppedBytes
	return base
}

// evaluateSpeech 根据单帧语音判定更新 gate 状态，保留 WebRTC ASR 的预滚和切句参数。
func (g *asrVoiceGate) evaluateSpeech(payload []byte, speech bool, energy, confidence, threshold float64, source string) asrVoiceGateDecision {
	base := asrVoiceGateDecision{Energy: energy, VADConfidence: confidence, VADThreshold: threshold, VADSource: source}
	if !g.speaking {
		g.pushPreRoll(payload)
		if speech {
			g.speechFrames++
		} else {
			g.speechFrames = 0
			g.droppedFrames++
			g.droppedBytes += len(payload)
			base.DroppedFrames = g.droppedFrames
			base.DroppedBytes = g.droppedBytes
			return base
		}
		if g.speechFrames < g.startFrames {
			g.droppedFrames++
			g.droppedBytes += len(payload)
			base.Speech = true
			base.DroppedFrames = g.droppedFrames
			base.DroppedBytes = g.droppedBytes
			return base
		}
		g.speaking = true
		g.silenceFrames = 0
		frames := g.drainPreRoll()
		base.WriteFrames = frames
		base.SpeechStart = true
		base.Speech = true
		base.DroppedFrames = g.droppedFrames
		base.DroppedBytes = g.droppedBytes
		return base
	}

	frame := append([]byte(nil), payload...)
	if speech {
		g.silenceFrames = 0
		g.speechFrames++
		base.WriteFrames = [][]byte{frame}
		base.Speech = true
		base.DroppedFrames = g.droppedFrames
		base.DroppedBytes = g.droppedBytes
		return base
	}
	g.silenceFrames++
	if g.silenceFrames >= g.endSilenceFrames {
		g.speaking = false
		g.speechFrames = 0
		g.silenceFrames = 0
		g.preRoll = nil
		base.WriteFrames = [][]byte{frame}
		base.SpeechEnd = true
		base.DroppedFrames = g.droppedFrames
		base.DroppedBytes = g.droppedBytes
		return base
	}
	base.WriteFrames = [][]byte{frame}
	base.DroppedFrames = g.droppedFrames
	base.DroppedBytes = g.droppedBytes
	return base
}

// resetVADState 清理当前通话在共享 VAD detector 中的状态。
func (s *Session) resetVADState() {
	if s == nil || s.asrGate == nil || s.asrGate.detector == nil || s.asrGate.vadSessionID == "" {
		return
	}
	s.asrGate.detector.Reset(s.asrGate.vadSessionID)
}

// pushPreRoll 保存语音开始前的少量音频，避免 VAD 启动延迟吃掉开头。
func (g *asrVoiceGate) pushPreRoll(payload []byte) {
	if g.preRollFrames <= 0 {
		return
	}
	g.preRoll = append(g.preRoll, append([]byte(nil), payload...))
	if len(g.preRoll) > g.preRollFrames {
		copy(g.preRoll, g.preRoll[len(g.preRoll)-g.preRollFrames:])
		g.preRoll = g.preRoll[:g.preRollFrames]
	}
}

// drainPreRoll 取出并清空预滚音频帧。
func (g *asrVoiceGate) drainPreRoll() [][]byte {
	frames := append([][]byte(nil), g.preRoll...)
	g.preRoll = nil
	return frames
}

// newASRClientFromProviderConfig 根据 WebSocket client_hello 上报的 provider 配置创建 ASR 客户端。
func newASRClientFromProviderConfig(configs map[model.CapabilityType]model.ProviderConfig) *asr.Client {
	config, ok := configs[model.CapabilityTypeASR]
	if !ok || config.Provider == "" {
		return nil
	}
	asrConfig := asrConfigFromProviderConfig(config)
	client := asr.NewClient(asrConfig)
	slog.Info("WebRTC ASR provider 已启用",
		slog.String("provider", client.Provider()),
		slog.Bool("endpointConfigured", asrConfig.Endpoint != ""),
		slog.String("model", asrConfig.Model),
		slog.String("language", asrConfig.Language),
	)
	return client
}

func newTMTClientFromProviderConfig(configs map[model.CapabilityType]model.ProviderConfig) *tmt.Client {
	config, ok := configs[model.CapabilityTypeTMT]
	if !ok || config.Provider == "" {
		return nil
	}
	translateConfig := tmtConfigFromProviderConfig(config)
	client := tmt.NewClient(translateConfig)
	slog.Info("WebRTC TMT provider 已启用",
		slog.String("provider", client.Provider()),
		slog.Bool("endpointConfigured", translateConfig.Endpoint != ""),
		slog.String("region", translateConfig.Region),
	)
	return client
}

// shouldForwardASRPartial 判断本次 WebRTC 会话是否需要把 ASR partial 也回传给业务 WebSocket。
func shouldForwardASRPartial(configs map[model.CapabilityType]model.ProviderConfig) bool {
	config, ok := configs[model.CapabilityTypeASR]
	if !ok {
		return false
	}
	for _, key := range []string{"forward_partial", "forwardPartial", "return_partial", "returnPartial", "partial_results", "partialResults"} {
		if truthyConfigValue(config.Params[key]) {
			return true
		}
	}
	return false
}

// truthyConfigValue 判断配置字符串是否表示开启。
func truthyConfigValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// asrResultForwardKey 生成 ASR 回调去重 key，同文本同 final 状态只转发一次。
func asrResultForwardKey(result asr.Result) string {
	state := "partial"
	if result.IsFinal {
		state = "final"
	}
	return state + ":" + strings.Join(strings.Fields(result.Text), "")
}

// newASRVoiceGateFromProviderConfig 根据 ASR provider 参数创建 WebRTC 本地 VAD 门控。
func newASRVoiceGateFromProviderConfig(configs map[model.CapabilityType]model.ProviderConfig) *asrVoiceGate {
	config, ok := configs[model.CapabilityTypeASR]
	if !ok || config.Provider != "tencent-asr" {
		return nil
	}
	if value, ok := firstProviderParam(config.Params, "local_vad", "localVad", "vad_enabled", "vadEnabled"); ok && !truthyConfigValue(value) {
		return &asrVoiceGate{enabled: false}
	}
	return &asrVoiceGate{
		enabled:          true,
		threshold:        providerFloatParam(config.Params, 0.012, "vad_threshold", "vadThreshold", "local_vad_threshold", "localVadThreshold"),
		startFrames:      providerIntParam(config.Params, 2, "vad_start_frames", "vadStartFrames", "speech_min_frames", "speechMinFrames"),
		endSilenceFrames: providerIntParam(config.Params, 35, "vad_end_silence_frames", "vadEndSilenceFrames", "silence_frames", "silenceFrames"),
		preRollFrames:    providerIntParam(config.Params, 8, "vad_preroll_frames", "vadPrerollFrames", "pre_roll_frames", "preRollFrames"),
	}
}

// firstProviderParam 按顺序读取 provider 参数中的第一个非空值。
func firstProviderParam(params map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if strings.TrimSpace(params[key]) != "" {
			return strings.TrimSpace(params[key]), true
		}
	}
	return "", false
}

// providerFloatParam 从 provider 参数读取 float 配置，非法或非正值时返回默认值。
func providerFloatParam(params map[string]string, fallback float64, keys ...string) float64 {
	value, ok := firstProviderParam(params, keys...)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// providerIntParam 从 provider 参数读取 int 配置，非法或非正值时返回默认值。
func providerIntParam(params map[string]string, fallback int, keys ...string) int {
	value, ok := firstProviderParam(params, keys...)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// pcm16RMSNormalized 计算 PCM16LE 单声道音频的 RMS 幅度，范围约为 0 到 1。
func pcm16RMSNormalized(payload []byte) float64 {
	samples := len(payload) / 2
	if samples == 0 {
		return 0
	}
	var squareSum float64
	for index := 0; index < samples; index++ {
		sample := float64(int16(binary.LittleEndian.Uint16(payload[index*2:]))) / 32768
		squareSum += sample * sample
	}
	return math.Sqrt(squareSum / float64(samples))
}

// asrConfigFromProviderConfig 将通用 ProviderConfig 转换为 ASR Config。
func asrConfigFromProviderConfig(provider model.ProviderConfig) asr.Config {
	params := cloneStringMap(provider.Params)
	if provider.Provider == "tencent-asr" {
		if params == nil {
			params = map[string]string{}
		}
		normalizeTencentASRParamsForWebRTCPcm(params)
	}
	return asr.Config{
		Provider: provider.Provider,
		APIKey:   firstConfigValue(provider.Secrets, "apiKey", "api_key", "token"),
		Endpoint: provider.Endpoint,
		Model:    firstConfigValue(params, "engine_model_type", "model"),
		Language: firstConfigValue(params, "language"),
		Params:   params,
		Secrets:  cloneStringMap(provider.Secrets),
	}
}

// tmtConfigFromProviderConfig 将通用 ProviderConfig 转换为翻译 Config。
func tmtConfigFromProviderConfig(provider model.ProviderConfig) tmt.Config {
	params := cloneStringMap(provider.Params)
	secrets := cloneStringMap(provider.Secrets)
	return tmt.Config{
		Provider: provider.Provider,
		APIKey:   firstConfigValue(secrets, "apiKey", "api_key", "token"),
		Endpoint: provider.Endpoint,
		Model:    firstConfigValue(params, "model"),
		Region:   firstConfigValue(params, "region"),
		Params:   params,
		Secrets:  secrets,
	}
}

// newASRAudioProcessorFromProviderConfig 为腾讯 ASR 创建 WebRTC Opus -> PCM16LE/16k 转码器。
func newASRAudioProcessorFromProviderConfig(configs map[model.CapabilityType]model.ProviderConfig) (*asrAudioProcessor, error) {
	config, ok := configs[model.CapabilityTypeASR]
	if !ok || config.Provider != "tencent-asr" {
		return nil, nil
	}
	return newOpusToPCM16Processor()
}

func newOpusToPCM16Processor() (*asrAudioProcessor, error) {
	decoder, err := gopus.NewDecoder(48000, 1)
	if err != nil {
		return nil, fmt.Errorf("初始化 Opus 解码器失败: %w", err)
	}
	return &asrAudioProcessor{opusDecoder: decoder}, nil
}

// normalizeTencentASRParamsForWebRTCPcm 把 WebRTC 上行音频转码后的 PCM 参数写给腾讯 ASR。
func normalizeTencentASRParamsForWebRTCPcm(params map[string]string) {
	if params == nil {
		return
	}
	params["voice_format"] = "pcm"
	delete(params, "input_sample_rate")
}

// decodeOpusToPCM16LE 把一包 WebRTC RTP Opus payload 解码为 16k 单声道 PCM16LE。
func (p *asrAudioProcessor) decodeOpusToPCM16LE(payload []byte) ([]byte, error) {
	if p == nil || p.opusDecoder == nil {
		return append([]byte(nil), payload...), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	const maxOpusFrameSamples = 5760
	decoded, err := p.opusDecoder.Decode(payload, maxOpusFrameSamples, false)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	pcm16k := resamplePCM16Nearest(decoded, 48000, 16000)
	return encodePCM16LE(pcm16k), nil
}

// encodePCM16LE 把 int16 采样写成小端 PCM 字节。
func encodePCM16LE(samples []int16) []byte {
	if len(samples) == 0 {
		return nil
	}
	out := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(out[index*2:], uint16(sample))
	}
	return out
}

// summarizeFramesForASR 汇总 ASR 帧的 callID 和字节数。
func summarizeFramesForASR(frames []asr.Frame) (string, int) {
	var callID string
	var bytes int
	for _, frame := range frames {
		if callID == "" {
			callID = frame.CallID
		}
		bytes += len(frame.Payload)
	}
	return callID, bytes
}

func previewASRText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// cloneStringMap 复制字符串 map。
func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func compactMetadata(values map[string]string) map[string]string {
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			delete(values, key)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func (s *Session) publishRequiredEvent(ctx context.Context, event model.DomainEvent) {
	if s.eventBus == nil {
		return
	}
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.eventBus.PublishRequired(publishCtx, pbxevents.Topic, event); err != nil {
		slog.WarnContext(ctx, "PBX required event 发布失败",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("eventType", string(event.Type)),
			slog.Any("error", err),
		)
	}
}

func (s *Session) publishBestEffortEvent(ctx context.Context, event model.DomainEvent) {
	if s.eventBus == nil {
		return
	}
	if err := s.eventBus.PublishBestEffort(ctx, pbxevents.Topic, event); err != nil {
		slog.DebugContext(ctx, "PBX best-effort event 发布失败",
			slog.String("connectionId", s.connectionID),
			slog.String("callId", s.callID),
			slog.String("eventType", string(event.Type)),
			slog.Any("error", err),
		)
	}
}

// firstConfigValue 从参数 map 中按顺序取第一个非空值。
func firstConfigValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if strings.TrimSpace(values[key]) != "" {
			return strings.TrimSpace(values[key])
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type iceCandidateSummary struct {
	Address   string
	Port      string
	Protocol  string
	Type      string
	TCPType   string
	RawPrefix string
}

// summarizeICECandidate 从 candidate 字符串中提取地址、端口、协议和类型，避免日志只剩一段难读的 SDP。
func summarizeICECandidate(raw string) iceCandidateSummary {
	text := strings.TrimSpace(strings.TrimPrefix(raw, "candidate:"))
	fields := strings.Fields(text)
	summary := iceCandidateSummary{RawPrefix: truncateLogValue(raw, 120)}
	if len(fields) >= 6 {
		summary.Protocol = fields[2]
		summary.Address = fields[4]
		summary.Port = fields[5]
	}
	for index, field := range fields {
		switch field {
		case "typ":
			if index+1 < len(fields) {
				summary.Type = fields[index+1]
			}
		case "tcptype":
			if index+1 < len(fields) {
				summary.TCPType = fields[index+1]
			}
		}
	}
	return summary
}

type iceStatsSummary struct {
	CandidatePairs []candidatePairSummary
	Local          []candidateStatSummary
	Remote         []candidateStatSummary
}

type candidatePairSummary struct {
	State             string
	Nominated         bool
	LocalCandidateID  string
	RemoteCandidateID string
	PacketsSent       uint32
	PacketsReceived   uint32
	BytesSent         uint64
	BytesReceived     uint64
}

type candidateStatSummary struct {
	ID            string
	IP            string
	Port          int32
	Protocol      string
	CandidateType string
}

// summarizeICEStats 汇总 Pion 统计中的 ICE candidate 和 pair，用于排查 failed 状态。
func summarizeICEStats(report pionwebrtc.StatsReport) iceStatsSummary {
	summary := iceStatsSummary{}
	for _, stat := range report {
		switch item := stat.(type) {
		case pionwebrtc.ICECandidatePairStats:
			summary.CandidatePairs = append(summary.CandidatePairs, candidatePairSummary{
				State:             string(item.State),
				Nominated:         item.Nominated,
				LocalCandidateID:  item.LocalCandidateID,
				RemoteCandidateID: item.RemoteCandidateID,
				PacketsSent:       item.PacketsSent,
				PacketsReceived:   item.PacketsReceived,
				BytesSent:         item.BytesSent,
				BytesReceived:     item.BytesReceived,
			})
		case pionwebrtc.ICECandidateStats:
			candidate := candidateStatSummary{
				ID:            item.ID,
				IP:            item.IP,
				Port:          item.Port,
				Protocol:      item.Protocol,
				CandidateType: item.CandidateType.String(),
			}
			if item.Type == pionwebrtc.StatsTypeLocalCandidate {
				summary.Local = append(summary.Local, candidate)
				continue
			}
			if item.Type == pionwebrtc.StatsTypeRemoteCandidate {
				summary.Remote = append(summary.Remote, candidate)
			}
		}
	}
	return summary
}

// startAudioWatch 在 WebRTC 连通后启动一次性音频等待诊断，帮助定位 ICE 已通但媒体未进入的问题。
func startAudioWatch(ctx context.Context, session *Session, trigger string) {
	if !session.audioWatchStarted.CompareAndSwap(false, true) {
		return
	}
	go func() {
		timer := time.NewTimer(audioWaitTimeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if session.audioPacketSeen.Load() {
			return
		}
		attrs := []slog.Attr{
			slog.String("connectionId", session.connectionID),
			slog.String("callId", session.callID),
			slog.String("userId", session.userID),
			slog.String("trigger", trigger),
			slog.Duration("waited", audioWaitTimeout),
			slog.String("peerState", session.peer.ConnectionState().String()),
			slog.String("iceState", session.peer.ICEConnectionState().String()),
			slog.String("signalingState", session.peer.SignalingState().String()),
			slog.Bool("audioTrackSeen", session.audioTrackSeen.Load()),
			slog.Bool("audioPacketSeen", session.audioPacketSeen.Load()),
		}
		if session.audioTrackSeen.Load() {
			slog.LogAttrs(ctx, slog.LevelWarn, "WebRTC 已收到音频轨道，但 10 秒内没有收到音频包", attrs...)
			return
		}
		slog.LogAttrs(ctx, slog.LevelWarn, "WebRTC ICE 已连通，但 10 秒内没有收到音频轨道", attrs...)
	}()
}

type audioSDPSummary struct {
	MediaCount int
	Directions []string
	Codecs     []string
	Ports      []int
	ParseError string
}

// summarizeAudioSDP 解析 SDP 中的音频媒体摘要，用于确认 offer/answer 是否允许前端发送音频。
func summarizeAudioSDP(raw string) audioSDPSummary {
	var desc pionsdp.SessionDescription
	if err := desc.UnmarshalString(raw); err != nil {
		return audioSDPSummary{ParseError: err.Error()}
	}
	summary := audioSDPSummary{}
	for _, media := range desc.MediaDescriptions {
		if media.MediaName.Media != "audio" {
			continue
		}
		summary.MediaCount++
		summary.Ports = append(summary.Ports, media.MediaName.Port.Value)
		summary.Directions = append(summary.Directions, audioDirection(&desc, media))
		for _, attr := range media.Attributes {
			if attr.Key != "rtpmap" {
				continue
			}
			parts := strings.Fields(attr.Value)
			if len(parts) >= 2 {
				summary.Codecs = append(summary.Codecs, parts[0]+":"+parts[1])
			}
		}
	}
	return summary
}

// audioDirection 读取媒体级或会话级方向属性；未声明时 SDP 默认 sendrecv。
func audioDirection(desc *pionsdp.SessionDescription, media *pionsdp.MediaDescription) string {
	for _, direction := range []string{"sendrecv", "sendonly", "recvonly", "inactive"} {
		if _, ok := media.Attribute(direction); ok {
			return direction
		}
	}
	for _, direction := range []string{"sendrecv", "sendonly", "recvonly", "inactive"} {
		if _, ok := desc.Attribute(direction); ok {
			return direction
		}
	}
	return "sendrecv"
}

// truncateLogValue 返回适合日志展示的截断字符串。
func truncateLogValue(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

// parseICECandidate 解析前端发来的纯 candidate 字符串或 JSON ICECandidateInit。
func parseICECandidate(raw string) (pionwebrtc.ICECandidateInit, error) {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "{") {
		var candidate pionwebrtc.ICECandidateInit
		if err := json.Unmarshal([]byte(text), &candidate); err != nil {
			return pionwebrtc.ICECandidateInit{}, err
		}
		return candidate, nil
	}
	return pionwebrtc.ICECandidateInit{Candidate: text}, nil
}
