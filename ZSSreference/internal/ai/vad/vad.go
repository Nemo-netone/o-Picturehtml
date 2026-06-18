//  语音活动检测(VAD)：Simple(RMS能量门控)+Silero(ONNX模型)双模式+预滚buffer+语音开始/结束判定
package vad

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	ProviderSimple = "simple"
	ProviderSilero = "silero"

	scoreLogEveryFrames = 50
)

type EventType string

const (
	EventSpeechStart EventType = "speech_start"
	EventSpeechEnd   EventType = "speech_end"
	EventSilence     EventType = "silence"
	EventNoise       EventType = "noise"
	EventBargeIn     EventType = "barge_in"
	EventEndpoint    EventType = "endpoint"
)

type Config struct {
	Provider             string
	ModelPath            string
	RuntimeLibraryPath   string
	SampleRate           int
	SpeechThreshold      float64
	SpeechMinFrames      int
	SilenceFrames        int
	BargeInFrames        int
	WindowFrames         int
	WindowSpeech         int
	InputName            string
	StateName            string
	SampleRateName       string
	OutputName           string
	NextStateName        string
	SileroWindowSamples  int
	SileroContextSamples int
	SileroStateSize      int
}

type Frame struct {
	CallID     string
	Payload    []byte
	TTSPlaying bool
}

type Event struct {
	CallID     string
	Type       EventType
	Confidence float64
	Energy     float64
}

type FrameScore struct {
	Confidence float64
	Energy     float64
	Scored     bool
}

type Detector struct {
	mu       sync.Mutex
	config   Config
	provider provider
	sessions map[string]*session
}

type session struct {
	speechFrames   int
	silenceFrames  int
	bargeFrames    int
	speaking       bool
	window         []bool
	onnxState      []float32
	onnxContext    []float32
	onnxPending    []float32
	scoreFrames    int
	lastPrediction prediction
}

type prediction struct {
	Confidence float64
	Energy     float64
	Scored     bool
}

type provider interface {
	Predict(frame Frame, state *session) (prediction, error)
	Close() error
}

// NewDetector 创建 VAD 检测器，按配置初始化 simple 或 Silero ONNX provider，并填入默认阈值参数。
func NewDetector(config Config) (*Detector, error) {
	config = normalizeConfig(config)
	if config.Provider == ProviderSilero && config.ModelPath != "" {
		if _, err := os.Stat(config.ModelPath); err != nil {
			return nil, err
		}
	}
	vadProvider, err := newProvider(config)
	if err != nil {
		return nil, err
	}
	slog.Info("VAD detector 已初始化",
		slog.String("provider", config.Provider),
		slog.String("modelPath", config.ModelPath),
		slog.Bool("modelConfigured", config.ModelPath != ""),
		slog.Bool("runtimeConfigured", config.RuntimeLibraryPath != ""),
		slog.Int("sampleRate", config.SampleRate),
		slog.Float64("speechThreshold", config.SpeechThreshold),
		slog.Int("speechMinFrames", config.SpeechMinFrames),
		slog.Int("silenceFrames", config.SilenceFrames),
		slog.Int("windowFrames", config.WindowFrames),
		slog.Int("windowSpeech", config.WindowSpeech),
	)
	return &Detector{config: config, provider: vadProvider, sessions: map[string]*session{}}, nil
}

// Process 对一帧音频做能量计算，并用滑动窗口平滑语音判定后返回 VAD 事件。
// 逻辑: 单帧先转成能量置信度，再进入窗口统计；speech_start/barge_in 依赖窗口内语音帧数，speech_end 依赖连续静音帧。
func (d *Detector) Process(frame Frame) []Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	if frame.CallID == "" {
		return nil
	}
	state := d.sessions[frame.CallID]
	if state == nil {
		state = &session{}
		d.sessions[frame.CallID] = state
	}

	prediction, err := d.provider.Predict(frame, state)
	if err != nil {
		slog.Warn("VAD provider 推理失败",
			slog.String("provider", d.config.Provider),
			slog.String("callId", frame.CallID),
			slog.Any("error", err),
		)
		return nil
	}
	energy := prediction.Energy
	confidence := prediction.Confidence
	if !prediction.Scored {
		return nil
	}
	rawSpeech := confidence >= d.config.SpeechThreshold
	windowSpeechCount := state.pushSpeech(rawSpeech, d.config.WindowFrames)
	windowSpeech := windowSpeechCount >= d.config.WindowSpeech
	events := make([]Event, 0, 2)

	if windowSpeech {
		state.speechFrames = windowSpeechCount
		if rawSpeech {
			state.silenceFrames = 0
		} else {
			state.silenceFrames++
		}
		if frame.TTSPlaying {
			state.bargeFrames++
			if windowSpeechCount >= d.config.BargeInFrames {
				events = append(events, Event{CallID: frame.CallID, Type: EventBargeIn, Confidence: confidence, Energy: energy})
			}
		}
		if state.speaking && !rawSpeech && state.silenceFrames >= d.config.SilenceFrames {
			state.speaking = false
			events = append(events, Event{CallID: frame.CallID, Type: EventSpeechEnd, Confidence: confidence, Energy: energy})
			events = append(events, Event{CallID: frame.CallID, Type: EventEndpoint, Confidence: confidence, Energy: energy})
			d.logEvents(events)
			return events
		}
		if !state.speaking && windowSpeechCount >= d.config.SpeechMinFrames {
			state.speaking = true
			events = append(events, Event{CallID: frame.CallID, Type: EventSpeechStart, Confidence: confidence, Energy: energy})
		}
		d.logEvents(events)
		return events
	}

	if !rawSpeech {
		state.speechFrames = 0
		state.silenceFrames++
	}
	state.bargeFrames = 0
	if state.speaking && state.silenceFrames >= d.config.SilenceFrames {
		state.speaking = false
		events = append(events, Event{CallID: frame.CallID, Type: EventSpeechEnd, Confidence: confidence, Energy: energy})
		events = append(events, Event{CallID: frame.CallID, Type: EventEndpoint, Confidence: confidence, Energy: energy})
		d.logEvents(events)
		return events
	}
	if energy == 0 {
		events = append(events, Event{CallID: frame.CallID, Type: EventSilence, Confidence: confidence, Energy: energy})
	} else {
		events = append(events, Event{CallID: frame.CallID, Type: EventNoise, Confidence: confidence, Energy: energy})
	}
	d.logEvents(events)
	return events
}

func (d *Detector) logEvents(events []Event) {
	for _, event := range events {
		switch event.Type {
		case EventSpeechStart, EventSpeechEnd, EventEndpoint, EventBargeIn:
			slog.Info("VAD 事件",
				slog.String("provider", d.config.Provider),
				slog.String("callId", event.CallID),
				slog.String("event", string(event.Type)),
				slog.Float64("confidence", event.Confidence),
				slog.Float64("energy", event.Energy),
			)
		}
	}
}

// Score 对单帧音频运行 VAD provider，返回原始置信度，供外部状态机自行做切句。
func (d *Detector) Score(frame Frame) (FrameScore, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if frame.CallID == "" {
		return FrameScore{}, errors.New("vad call id is required")
	}
	state := d.sessions[frame.CallID]
	if state == nil {
		state = &session{}
		d.sessions[frame.CallID] = state
	}
	prediction, err := d.provider.Predict(frame, state)
	if err != nil {
		slog.Warn("VAD Score provider 推理失败",
			slog.String("provider", d.config.Provider),
			slog.String("callId", frame.CallID),
			slog.Int("payloadBytes", len(frame.Payload)),
			slog.Any("error", err),
		)
		return FrameScore{}, err
	}
	state.scoreFrames++
	d.logScore(frame, state.scoreFrames, prediction)
	return FrameScore{Confidence: prediction.Confidence, Energy: prediction.Energy, Scored: prediction.Scored}, nil
}

func (d *Detector) logScore(frame Frame, frameCount int, prediction prediction) {
	if frameCount != 1 && frameCount%scoreLogEveryFrames != 0 {
		return
	}
	slog.Info("VAD Score 已完成",
		slog.String("provider", d.config.Provider),
		slog.String("callId", frame.CallID),
		slog.Int("scoredFrames", frameCount),
		slog.Int("payloadBytes", len(frame.Payload)),
		slog.Float64("confidence", prediction.Confidence),
		slog.Float64("energy", prediction.Energy),
		slog.Float64("threshold", d.config.SpeechThreshold),
		slog.Bool("speech", prediction.Confidence >= d.config.SpeechThreshold),
		slog.Bool("scored", prediction.Scored),
		slog.Bool("ttsPlaying", frame.TTSPlaying),
	)
}

// SpeechThreshold 返回 detector 当前使用的语音判定阈值。
func (d *Detector) SpeechThreshold() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.config.SpeechThreshold
}

// pushSpeech 把当前帧语音判定写入滑动窗口，并返回窗口内语音帧数量。
func (s *session) pushSpeech(speech bool, windowFrames int) int {
	s.window = append(s.window, speech)
	if len(s.window) > windowFrames {
		copy(s.window, s.window[len(s.window)-windowFrames:])
		s.window = s.window[:windowFrames]
	}
	count := 0
	for _, item := range s.window {
		if item {
			count++
		}
	}
	return count
}

// Close 清空所有 VAD 会话状态。
func (d *Detector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sessions == nil {
		return errors.New("detector already closed")
	}
	if d.provider != nil {
		_ = d.provider.Close()
	}
	d.sessions = map[string]*session{}
	return nil
}

// Reset 清理指定通话的 VAD 状态，避免模型 hidden state 在通话结束后串到下一次会话。
func (d *Detector) Reset(callID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, callID)
	slog.Info("VAD 会话状态已重置",
		slog.String("provider", d.config.Provider),
		slog.String("callId", callID),
	)
}

// SessionCount 返回当前活跃的 VAD 会话数。
func (d *Detector) SessionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sessions)
}

// energy 计算 PCM16LE 音频的 RMS 能量百分比，非 PCM 偶发输入则退化为字节平均能量。
func energy(payload []byte) float64 {
	if len(payload) == 0 {
		return 0
	}
	if len(payload) >= 2 {
		samples := len(payload) / 2
		var squareSum float64
		for index := 0; index < samples; index++ {
			sample := float64(int16(binary.LittleEndian.Uint16(payload[index*2:]))) / 32768
			squareSum += sample * sample
		}
		return math.Sqrt(squareSum/float64(samples)) * 100
	}
	var total float64
	for _, b := range payload {
		total += float64(b)
	}
	return total / float64(len(payload)) / 255 * 100
}

// normalizeConfig 填充 VAD provider、Silero 输入输出名和状态机默认值。
func normalizeConfig(config Config) Config {
	if config.Provider == "" {
		config.Provider = ProviderSimple
	}
	if config.SampleRate <= 0 {
		config.SampleRate = 16000
	}
	if config.SpeechThreshold <= 0 {
		config.SpeechThreshold = 0.5
	}
	if config.SpeechMinFrames <= 0 {
		config.SpeechMinFrames = 2
	}
	if config.SilenceFrames <= 0 {
		config.SilenceFrames = 2
	}
	if config.BargeInFrames <= 0 {
		config.BargeInFrames = 2
	}
	if config.WindowFrames <= 0 {
		config.WindowFrames = max(config.SpeechMinFrames, 3)
	}
	if config.WindowSpeech <= 0 {
		config.WindowSpeech = config.SpeechMinFrames
	}
	if config.WindowSpeech > config.WindowFrames {
		config.WindowSpeech = config.WindowFrames
	}
	if config.InputName == "" {
		config.InputName = "input"
	}
	if config.StateName == "" {
		config.StateName = "state"
	}
	if config.SampleRateName == "" {
		config.SampleRateName = "sr"
	}
	if config.OutputName == "" {
		config.OutputName = "output"
	}
	if config.NextStateName == "" {
		config.NextStateName = "stateN"
	}
	config = normalizeSileroWindowConfig(config)
	if config.SileroStateSize <= 0 {
		config.SileroStateSize = 128
	}
	return config
}

func normalizeSileroWindowConfig(config Config) Config {
	if config.SileroWindowSamples <= 0 {
		if config.SampleRate == 8000 {
			config.SileroWindowSamples = 256
		} else {
			config.SileroWindowSamples = 512
		}
	}
	if config.SileroContextSamples <= 0 {
		if config.SampleRate == 8000 {
			config.SileroContextSamples = 32
		} else {
			config.SileroContextSamples = 64
		}
	}
	return config
}

// newProvider 按配置创建 VAD provider。
func newProvider(config Config) (provider, error) {
	switch config.Provider {
	case ProviderSimple:
		return simpleProvider{}, nil
	case ProviderSilero:
		return newSileroProvider(config)
	default:
		return nil, fmt.Errorf("unsupported vad provider: %s", config.Provider)
	}
}

type simpleProvider struct{}

// Predict 用 PCM RMS 能量生成简单 VAD 置信度。
func (simpleProvider) Predict(frame Frame, _ *session) (prediction, error) {
	energy := energy(frame.Payload)
	return prediction{Confidence: math.Min(1, energy/10), Energy: energy, Scored: true}, nil
}

// Close 释放 simple provider；无资源需要处理。
func (simpleProvider) Close() error {
	return nil
}

type sileroProvider struct {
	config  Config
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex
}

var onnxRuntimeMu sync.Mutex

// newSileroProvider 初始化 ONNX Runtime 环境并加载 Silero VAD 模型。
func newSileroProvider(config Config) (*sileroProvider, error) {
	if config.ModelPath == "" {
		return nil, errors.New("silero vad model path is required")
	}
	if err := initializeONNXRuntime(config.RuntimeLibraryPath); err != nil {
		return nil, err
	}
	session, err := ort.NewDynamicAdvancedSession(
		config.ModelPath,
		[]string{config.InputName, config.StateName, config.SampleRateName},
		[]string{config.OutputName, config.NextStateName},
		nil,
	)
	if err != nil {
		return nil, err
	}
	return &sileroProvider{config: config, session: session}, nil
}

// initializeONNXRuntime 设置 shared library 路径并初始化 ONNX Runtime 全局环境。
func initializeONNXRuntime(sharedLibraryPath string) error {
	onnxRuntimeMu.Lock()
	defer onnxRuntimeMu.Unlock()
	if ort.IsInitialized() {
		return nil
	}
	if sharedLibraryPath != "" {
		ort.SetSharedLibraryPath(sharedLibraryPath)
	}
	return ort.InitializeEnvironment(ort.WithLogLevelWarning())
}

// Predict 将 PCM16LE 音频切成 Silero 输入窗口，运行 ONNX 推理并返回语音概率。
func (p *sileroProvider) Predict(frame Frame, state *session) (prediction, error) {
	samples := pcm16LEPayloadToFloat32(frame.Payload)
	if len(samples) == 0 {
		return prediction{Energy: 0, Confidence: state.lastPrediction.Confidence}, nil
	}
	energy := pcmFloatEnergy(samples)
	windows := appendSileroSamples(state, samples, p.config.SileroWindowSamples)
	if len(windows) == 0 {
		return prediction{Energy: energy, Confidence: state.lastPrediction.Confidence}, nil
	}
	var scored prediction
	for _, window := range windows {
		result, err := p.predictWindow(window, state)
		if err != nil {
			return prediction{}, err
		}
		scored = result
	}
	state.lastPrediction = scored
	return scored, nil
}

func (p *sileroProvider) predictWindow(samples []float32, state *session) (prediction, error) {
	inputSamples := sileroInputWithContext(samples, state, p.config.SileroContextSamples)
	if len(state.onnxState) != 2*p.config.SileroStateSize {
		state.onnxState = make([]float32, 2*p.config.SileroStateSize)
	}
	inputTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(inputSamples))), inputSamples)
	if err != nil {
		return prediction{}, err
	}
	defer inputTensor.Destroy()
	stateTensor, err := ort.NewTensor(ort.NewShape(2, 1, int64(p.config.SileroStateSize)), append([]float32(nil), state.onnxState...))
	if err != nil {
		return prediction{}, err
	}
	defer stateTensor.Destroy()
	srTensor, err := ort.NewTensor(ort.NewShape(1), []int64{int64(p.config.SampleRate)})
	if err != nil {
		return prediction{}, err
	}
	defer srTensor.Destroy()
	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
	if err != nil {
		return prediction{}, err
	}
	defer outputTensor.Destroy()
	nextStateTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(2, 1, int64(p.config.SileroStateSize)))
	if err != nil {
		return prediction{}, err
	}
	defer nextStateTensor.Destroy()

	p.mu.Lock()
	err = p.session.Run([]ort.Value{inputTensor, stateTensor, srTensor}, []ort.Value{outputTensor, nextStateTensor})
	p.mu.Unlock()
	if err != nil {
		return prediction{}, err
	}
	copy(state.onnxState, nextStateTensor.GetData())
	confidence := 0.0
	if out := outputTensor.GetData(); len(out) > 0 {
		confidence = float64(out[0])
	}
	state.onnxContext = trailingSamples(inputSamples, p.config.SileroContextSamples)
	return prediction{Confidence: clamp01(confidence), Energy: pcmFloatEnergy(samples), Scored: true}, nil
}

// Close 释放 Silero ONNX session。
func (p *sileroProvider) Close() error {
	if p == nil || p.session == nil {
		return nil
	}
	return p.session.Destroy()
}

// pcm16LEToFloat32 将 PCM16LE 音频转换为 Silero 需要的 float32 单声道窗口，不足时补零。
func pcm16LEToFloat32(payload []byte, samples int) []float32 {
	if samples <= 0 {
		return nil
	}
	out := make([]float32, samples)
	limit := len(payload) / 2
	if limit > samples {
		limit = samples
	}
	for index := 0; index < limit; index++ {
		raw := int16(binary.LittleEndian.Uint16(payload[index*2:]))
		out[index] = float32(raw) / 32768
	}
	return out
}

func pcm16LEPayloadToFloat32(payload []byte) []float32 {
	samples := len(payload) / 2
	if samples <= 0 {
		return nil
	}
	out := make([]float32, samples)
	for index := 0; index < samples; index++ {
		raw := int16(binary.LittleEndian.Uint16(payload[index*2:]))
		out[index] = float32(raw) / 32768
	}
	return out
}

func appendSileroSamples(state *session, samples []float32, windowSamples int) [][]float32 {
	if state == nil || windowSamples <= 0 || len(samples) == 0 {
		return nil
	}
	state.onnxPending = append(state.onnxPending, samples...)
	windows := make([][]float32, 0, len(state.onnxPending)/windowSamples)
	for len(state.onnxPending) >= windowSamples {
		window := append([]float32(nil), state.onnxPending[:windowSamples]...)
		windows = append(windows, window)
		copy(state.onnxPending, state.onnxPending[windowSamples:])
		state.onnxPending = state.onnxPending[:len(state.onnxPending)-windowSamples]
	}
	return windows
}

// sileroInputWithContext 拼出 Silero ONNX 需要的上下文窗口：上一帧尾部 context + 当前帧。
func sileroInputWithContext(samples []float32, state *session, contextSamples int) []float32 {
	if contextSamples <= 0 {
		return append([]float32(nil), samples...)
	}
	context := state.onnxContext
	if len(context) != contextSamples {
		context = make([]float32, contextSamples)
	}
	out := make([]float32, 0, contextSamples+len(samples))
	out = append(out, context...)
	out = append(out, samples...)
	return out
}

func trailingSamples(samples []float32, count int) []float32 {
	if count <= 0 {
		return nil
	}
	out := make([]float32, count)
	if len(samples) <= count {
		copy(out[count-len(samples):], samples)
		return out
	}
	copy(out, samples[len(samples)-count:])
	return out
}

// pcmFloatEnergy 返回 PCM float 窗口的平均绝对幅度，便于沿用原事件 energy 字段。
func pcmFloatEnergy(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var total float64
	for _, sample := range samples {
		if sample < 0 {
			sample = -sample
		}
		total += float64(sample)
	}
	return total / float64(len(samples))
}

// clamp01 把模型输出概率限制在 [0,1]，避免异常输出污染 VAD 状态机。
func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

