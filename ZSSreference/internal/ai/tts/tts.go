//  TTS合成抽象接口：Client→ Synthesize方法
package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

var ErrAuth = errors.New("tts auth error")

type Config struct {
	Provider        string
	APIKey          string
	Endpoint        string
	Model           string
	Params          map[string]string
	Secrets         map[string]string
	Voices          []string
	Languages       []string
	SampleRates     []int
	DefaultVoice    string
	DefaultLanguage string
	DefaultFormat   string
	DefaultRate     int
	FirstChunkDelay time.Duration
	ChunkDelay      time.Duration
	MaxSegmentRunes int
	MaxConcurrency  int
}

type Options struct {
	CallID      string
	UtteranceID string
	Voice       string
	Language    string
	Speed       float64
	Volume      float64
	SampleRate  int
	Format      string
}

type Request struct {
	Text     string
	Segments []string
	Options  Options
	Config   Config
}

type Chunk struct {
	model.TTSChunk
	Text     string
	Provider string
	Voice    string
	Language string
	Speed    float64
	Volume   float64
}

type Metrics struct {
	FirstChunkLatency time.Duration
	Bytes             int
	CancelCount       int
}

type Provider interface {
	Name() string
	Synthesize(ctx context.Context, req Request) ([]Chunk, error)
}

type ProviderFactory func(config Config) Provider

type Client struct {
	config   Config
	provider Provider

	mu      sync.Mutex
	active  map[string]context.CancelFunc
	metrics Metrics
}

var providerRegistry = struct {
	sync.RWMutex
	factories map[string]ProviderFactory
}{
	factories: map[string]ProviderFactory{},
}

// init 注册所有内置 TTS provider 工厂到全局 registry。
func init() {
	// mock 工厂
	RegisterProvider("mock", func(config Config) Provider {
		return NewMockProvider(config)
	})
	// Edge TTS 工厂
	RegisterProvider("edge", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// CosyVoice TTS 工厂
	RegisterProvider("cosyvoice", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// Aliyun TTS 工厂
	RegisterProvider("aliyun", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// Azure TTS 工厂
	RegisterProvider("azure", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// 腾讯云 TTS 工厂
	RegisterProvider("tencent-tts", func(config Config) Provider {
		return NewTencentProvider(config)
	})
}

// NewClient 根据配置创建 TTS 客户端（从全局 registry 查找 provider）。
func NewClient(config Config) *Client {
	config = normalizeConfig(config)
	return &Client{config: config, provider: providerFor(config), active: map[string]context.CancelFunc{}}
}

// NewClientWithProvider 创建使用指定 provider 的 TTS 客户端（注入方式，用于测试）。
func NewClientWithProvider(config Config, provider Provider) *Client {
	config = normalizeConfig(config)
	if provider == nil {
		provider = providerFor(config)
	}
	return &Client{config: config, provider: provider, active: map[string]context.CancelFunc{}}
}

// RegisterProvider 注册一个新的 TTS provider 工厂。
func RegisterProvider(name string, factory ProviderFactory) {
	if name == "" || factory == nil {
		return
	}
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.factories[name] = factory
}

// normalizeConfig 填充 TTS 配置默认值。
func normalizeConfig(config Config) Config {
	if config.Provider == "" {
		config.Provider = "mock"
	}
	if config.APIKey == "" {
		config.APIKey = "ok"
	}
	if config.DefaultVoice == "" {
		config.DefaultVoice = "mock-voice"
	}
	if config.DefaultLanguage == "" {
		config.DefaultLanguage = "en-US"
	}
	if config.DefaultFormat == "" {
		config.DefaultFormat = "pcm"
	}
	if config.DefaultRate == 0 {
		config.DefaultRate = 16000
	}
	if config.MaxSegmentRunes <= 0 {
		config.MaxSegmentRunes = 80
	}
	if len(config.Voices) == 0 {
		config.Voices = []string{config.DefaultVoice}
	}
	if len(config.Languages) == 0 {
		config.Languages = []string{config.DefaultLanguage}
	}
	if len(config.SampleRates) == 0 {
		config.SampleRates = []int{config.DefaultRate}
	}
	return config
}

// providerFor 按名称查找 provider 工厂，未找到返回 ThirdPartyProvider。
func providerFor(config Config) Provider {
	providerRegistry.RLock()
	factory := providerRegistry.factories[config.Provider]
	providerRegistry.RUnlock()
	if factory != nil {
		return factory(config)
	}
	return NewThirdPartyProvider(config)
}

// Provider 返回当前 provider 名称。
func (c *Client) Provider() string {
	return c.provider.Name()
}

// Synthesize 合成文本为语音：鉴权 → 分段 → 调用 provider → 记录指标。
// 逻辑:
// 1. API key 明确无效时立即返回鉴权错误。
// 2. 合并默认音色、语言、格式、采样率等参数。
// 3. 为当前 call 注册可取消上下文，barge-in 时由 Cancel 触发。
// 4. 分句后委托 Provider 合成，返回后统一记录首包延迟、字节数和取消次数。
func (c *Client) Synthesize(ctx context.Context, text string, options Options) ([]Chunk, error) {
	if c.config.APIKey == "bad" {
		slog.WarnContext(ctx, "TTS 鉴权被拒绝",
			slog.String("provider", c.Provider()),
			slog.String("callId", options.CallID),
			slog.String("utteranceId", options.UtteranceID),
		)
		return nil, ErrAuth
	}
	options = c.applyDefaults(options)
	streamCtx, cancel := context.WithCancel(ctx)
	c.register(options.CallID, cancel)
	defer c.release(options.CallID)

	start := time.Now()
	segments := SegmentText(text, c.config.MaxSegmentRunes)
	slog.InfoContext(ctx, "收到 TTS 输入",
		slog.String("provider", c.Provider()),
		slog.String("callId", options.CallID),
		slog.String("utteranceId", options.UtteranceID),
		slog.String("model", c.config.Model),
		slog.String("voice", options.Voice),
		slog.String("language", options.Language),
		slog.String("format", options.Format),
		slog.Int("sampleRate", options.SampleRate),
		slog.Int("textRunes", utf8.RuneCountInString(text)),
		slog.String("textPreview", previewText(text, 80)),
		slog.Int("segmentCount", len(segments)),
		slog.Bool("endpointConfigured", c.config.Endpoint != ""),
	)
	chunks, err := c.provider.Synthesize(streamCtx, Request{Text: text, Segments: segments, Options: options, Config: c.config})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.markCancelled()
			slog.WarnContext(ctx, "TTS 合成被中断",
				slog.String("provider", c.Provider()),
				slog.String("callId", options.CallID),
				slog.String("utteranceId", options.UtteranceID),
				slog.Int("chunkCount", len(chunks)),
				slog.Int("audioBytes", totalChunkBytes(chunks)),
				slog.Duration("elapsed", time.Since(start)),
				slog.Any("error", err),
			)
		} else {
			slog.ErrorContext(ctx, "TTS 服务商调用失败",
				slog.String("provider", c.Provider()),
				slog.String("callId", options.CallID),
				slog.String("utteranceId", options.UtteranceID),
				slog.Int("chunkCount", len(chunks)),
				slog.Int("audioBytes", totalChunkBytes(chunks)),
				slog.Duration("elapsed", time.Since(start)),
				slog.Any("error", err),
			)
		}
		return chunks, err
	}
	elapsed := time.Since(start)
	if len(chunks) > 0 {
		c.recordFirstChunk(elapsed)
	}
	for _, chunk := range chunks {
		c.addBytes(len(chunk.Audio))
		slog.InfoContext(ctx, "输出 TTS 音频块",
			slog.String("provider", chunk.Provider),
			slog.String("callId", chunk.CallID),
			slog.String("utteranceId", chunk.UtteranceID),
			slog.Int("sequence", chunk.Sequence),
			slog.Bool("isLast", chunk.IsLast),
			slog.String("format", chunk.Format),
			slog.Int("sampleRate", chunk.SampleRate),
			slog.Int("audioBytes", len(chunk.Audio)),
			slog.Int("textRunes", utf8.RuneCountInString(chunk.Text)),
			slog.String("textPreview", previewText(chunk.Text, 80)),
		)
	}
	slog.InfoContext(ctx, "收到 TTS 输出",
		slog.String("provider", c.Provider()),
		slog.String("callId", options.CallID),
		slog.String("utteranceId", options.UtteranceID),
		slog.Int("chunkCount", len(chunks)),
		slog.Int("audioBytes", totalChunkBytes(chunks)),
		slog.Duration("elapsed", elapsed),
	)
	return chunks, nil
}

// Cancel 取消某通话的 TTS 合成（用于 barge-in）。
func (c *Client) Cancel(callID string) {
	c.mu.Lock()
	cancel := c.active[callID]
	c.mu.Unlock()
	if cancel != nil {
		slog.Info("TTS 取消请求已收到",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
		)
		cancel()
		return
	}
	slog.Debug("TTS 取消已跳过",
		slog.String("provider", c.Provider()),
		slog.String("callId", callID),
		slog.String("reason", "未找到活跃合成流"),
	)
}

// ActiveStreams 返回当前活跃的合成流数量。
func (c *Client) ActiveStreams() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.active)
}

// Metrics 返回 TTS 指标快照。
func (c *Client) Metrics() Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.metrics
}

// applyDefaults 将请求级参数与客户端默认参数合并。
func (c *Client) applyDefaults(options Options) Options {
	if options.Voice == "" {
		options.Voice = c.config.DefaultVoice
	}
	if options.Language == "" {
		options.Language = c.config.DefaultLanguage
	}
	if options.Format == "" {
		options.Format = c.config.DefaultFormat
	}
	if options.SampleRate == 0 {
		options.SampleRate = c.config.DefaultRate
	}
	if options.Speed == 0 {
		options.Speed = 1
	}
	if options.Volume == 0 {
		options.Volume = 1
	}
	return options
}

// register 注册按 callID 取消的回调。
func (c *Client) register(callID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if callID == "" {
		callID = "default"
	}
	c.active[callID] = cancel
}

// release 释放 callID 的 cancel 注册。
func (c *Client) release(callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if callID == "" {
		callID = "default"
	}
	delete(c.active, callID)
}

// recordFirstChunk 记录首包延迟。
func (c *Client) recordFirstChunk(latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.FirstChunkLatency = latency
}

// addBytes 累加输出字节数。
func (c *Client) addBytes(bytes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.Bytes += bytes
}

// markCancelled 累加取消次数。
func (c *Client) markCancelled() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.CancelCount++
}

// totalChunkBytes 统计 TTS 输出音频总字节数，用于确认 provider 是否有音频信号输出。
func totalChunkBytes(chunks []Chunk) int {
	var bytes int
	for _, chunk := range chunks {
		bytes += len(chunk.Audio)
	}
	return bytes
}

// previewText 生成有限长度的文本预览，避免日志输出过长回复内容。
func previewText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes]) + "..."
}

// SegmentText 按句末标点（中英文）或最大字符数分段文本。
// 逻辑:
// 1. 去掉首尾空白，空文本直接返回空切片。
// 2. 遇到中英文句末标点立即切段。
// 3. 单段超过 maxRunes 时强制切段，防止 provider 请求过大。
func SegmentText(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 80
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var segments []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		value := strings.TrimSpace(current.String())
		if isBoundary(r) || utf8.RuneCountInString(value) >= maxRunes {
			if value != "" {
				segments = append(segments, value)
			}
			current.Reset()
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		segments = append(segments, tail)
	}
	return segments
}

// Capability 构建 TTS 能力的注册描述。
func Capability(id string, config Config) model.Capability {
	client := NewClient(config)
	if id == "" {
		id = "tts-worker"
	}
	return model.Capability{
		ID:             id,
		Type:           model.CapabilityTypeTTS,
		Protocol:       "grpc",
		Languages:      append([]string(nil), client.config.Languages...),
		Models:         append([]string(nil), client.config.Voices...),
		Streaming:      true,
		MaxConcurrency: max(1, client.config.MaxConcurrency),
		Labels: map[string]string{
			"provider": client.Provider(),
			"format":   client.config.DefaultFormat,
		},
	}
}

// isSegmentBoundary 判断字符是否为句子分隔符。
func isBoundary(r rune) bool {
	switch r {
	case '.', '!', '?', ';', ':', '\n', '。', '！', '？', '；':
		return true
	default:
		return false
	}
}

type MockProvider struct {
	config Config
	name   string
}

// NewMockProvider 创建 mock TTS provider（测试用）。
func NewMockProvider(config Config) *MockProvider {
	config = normalizeConfig(config)
	return &MockProvider{config: config, name: config.Provider}
}

// Name 返回 provider 名称。
func (p *MockProvider) Name() string {
	if p.name == "" {
		return "mock"
	}
	return p.name
}

// Synthesize 生成确定性 mock 音频块（provider|voice|text），按分段延迟模拟首包和分片耗时。
// 逻辑：逐段等待首包或分片延迟；ctx 取消时返回已生成 chunk；正常时用 provider、音色和文本构造音频 payload。
func (p *MockProvider) Synthesize(ctx context.Context, req Request) ([]Chunk, error) {
	chunks := make([]Chunk, 0, len(req.Segments))
	for index, segment := range req.Segments {
		delay := p.config.ChunkDelay
		if index == 0 {
			delay = p.config.FirstChunkDelay
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return chunks, ctx.Err()
			}
		}
		if err := ctx.Err(); err != nil {
			return chunks, err
		}
		payload := []byte(fmt.Sprintf("%s|%s|%s", p.Name(), req.Options.Voice, segment))
		chunks = append(chunks, Chunk{
			TTSChunk: model.TTSChunk{
				CallID:      req.Options.CallID,
				UtteranceID: req.Options.UtteranceID,
				Audio:       payload,
				Format:      req.Options.Format,
				SampleRate:  req.Options.SampleRate,
				Sequence:    index,
				IsLast:      index == len(req.Segments)-1,
			},
			Text:     segment,
			Provider: p.Name(),
			Voice:    req.Options.Voice,
			Language: req.Options.Language,
			Speed:    req.Options.Speed,
			Volume:   req.Options.Volume,
		})
	}
	return chunks, nil
}

type ThirdPartyProvider struct {
	mock *MockProvider
}

// NewThirdPartyProvider 创建第三方 provider 占位实现（当前委托给 mock）。
func NewThirdPartyProvider(config Config) *ThirdPartyProvider {
	return &ThirdPartyProvider{mock: NewMockProvider(config)}
}

// Name 返回 provider 名称。
func (p *ThirdPartyProvider) Name() string {
	return p.mock.Name()
}

// Synthesize 当前委托给 mock，真实接入时替换此处实现。
func (p *ThirdPartyProvider) Synthesize(ctx context.Context, req Request) ([]Chunk, error) {
	return p.mock.Synthesize(ctx, req)
}

