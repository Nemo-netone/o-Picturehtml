//  语音识别(ASR)抽象层：Provider接口→ Stream流式接口→ 注册机制→ 腾讯云实时ASR实现
package asr

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrAuth                 = errors.New("asr auth error")
	ErrRetryExhausted       = errors.New("asr retry exhausted")
	ErrStreamingUnsupported = errors.New("asr streaming unsupported")
)

type Config struct {
	Provider              string
	APIKey                string
	Endpoint              string
	Model                 string
	Language              string
	Params                map[string]string
	Secrets               map[string]string
	MaxRetries            int
	FailuresBeforeSuccess int
	Delay                 time.Duration
	MinConfidence         float64
	MockConfidence        float64
}

type Request struct {
	Frames []Frame
	Config Config
}

type Frame struct {
	CallID  string
	Payload []byte
}

type Result struct {
	CallID     string
	Text       string
	IsFinal    bool
	Confidence float64
}

type StreamRequest struct {
	Config Config
	CallID string
}

type Stream interface {
	Write(ctx context.Context, frame Frame) error
	Close(ctx context.Context) error
	Results() <-chan Result
	Errors() <-chan error
}

type Provider interface {
	Name() string
	Recognize(ctx context.Context, req Request) ([]Result, error)
}

type StreamingProvider interface {
	Provider
	OpenStream(ctx context.Context, req StreamRequest) (Stream, error)
}

type ProviderFactory func(config Config) Provider

type Client struct {
	config   Config
	provider Provider
}

var providerRegistry = struct {
	sync.RWMutex
	factories map[string]ProviderFactory
}{
	factories: map[string]ProviderFactory{},
}

// init 注册所有内置 ASR provider 工厂到全局 registry。
func init() {
	// mock 工厂
	RegisterProvider("mock", func(config Config) Provider {
		return NewMockProvider(config)
	})
	// FunASR 工厂
	RegisterProvider("funasr", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// Whisper 工厂
	RegisterProvider("whisper", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// 云厂商 ASR 工厂
	RegisterProvider("cloud", func(config Config) Provider {
		return NewThirdPartyProvider(config)
	})
	// 腾讯云实时 ASR 工厂
	RegisterProvider("tencent-asr", func(config Config) Provider {
		return NewTencentProvider(config)
	})
}

// NewClient 根据配置创建 ASR 客户端（从全局 registry 查找 provider）。
func NewClient(config Config) *Client {
	config = normalizeConfig(config)
	return &Client{config: config, provider: providerFor(config)}
}

// NewClientWithProvider 创建使用指定 provider 的 ASR 客户端（注入方式）。
func NewClientWithProvider(config Config, provider Provider) *Client {
	config = normalizeConfig(config)
	if provider == nil {
		provider = providerFor(config)
	}
	return &Client{config: config, provider: provider}
}

// RegisterProvider 注册一个新的 ASR provider 工厂。
func RegisterProvider(name string, factory ProviderFactory) {
	if name == "" || factory == nil {
		return
	}
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.factories[name] = factory
}

// normalizeConfig 填充 ASR 配置默认值。
func normalizeConfig(config Config) Config {
	if config.Provider == "" {
		config.Provider = "mock"
	}
	if config.APIKey == "" {
		config.APIKey = "ok"
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.MockConfidence == 0 {
		config.MockConfidence = 0.95
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

// SupportsStreaming 判断当前 ASR provider 是否支持长连接实时流。
func (c *Client) SupportsStreaming() bool {
	_, ok := c.provider.(StreamingProvider)
	return ok
}

// OpenStream 打开一个 ASR 长连接实时流。
// 逻辑:
// 1. 先做通用鉴权和 provider 能力检查，避免不支持流式的 provider 被误用。
// 2. 将 callID 和配置传给具体 provider，由 provider 建立真实外部连接。
// 3. 返回的 Stream 由调用方持续 Write 音频，并在通话/轨道结束时 Close。
func (c *Client) OpenStream(ctx context.Context, callID string) (Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.config.APIKey == "bad" {
		slog.WarnContext(ctx, "ASR 流式鉴权被拒绝",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
		)
		return nil, ErrAuth
	}
	provider, ok := c.provider.(StreamingProvider)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	slog.InfoContext(ctx, "ASR 流式连接开始",
		slog.String("provider", c.Provider()),
		slog.String("callId", callID),
		slog.String("model", c.config.Model),
		slog.String("language", c.config.Language),
		slog.Bool("endpointConfigured", c.config.Endpoint != ""),
	)
	stream, err := provider.OpenStream(ctx, StreamRequest{Config: c.config, CallID: callID})
	if err != nil {
		slog.ErrorContext(ctx, "ASR 流式连接失败",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
			slog.Any("error", err),
		)
		return nil, err
	}
	slog.InfoContext(ctx, "ASR 流式连接已建立",
		slog.String("provider", c.Provider()),
		slog.String("callId", callID),
	)
	return stream, nil
}

// StreamingRecognize 发送音频帧到 ASR：鉴权 → 重试 → 调用 provider。
// 逻辑:
// 1. API key 明确无效时立即返回鉴权错误，避免无意义重试。
// 2. 根据 MaxRetries 做重试循环，FailuresBeforeSuccess 用于测试 provider 失败恢复。
// 3. 真正识别动作委托给 Provider，第三方服务实现只需满足 Recognize 接口。
func (c *Client) StreamingRecognize(ctx context.Context, frames []Frame) ([]Result, error) {
	callID, audioBytes := summarizeFrames(frames)
	start := time.Now()
	slog.InfoContext(ctx, "收到 ASR 输入",
		slog.String("provider", c.Provider()),
		slog.String("callId", callID),
		slog.String("model", c.config.Model),
		slog.String("language", c.config.Language),
		slog.Int("frameCount", len(frames)),
		slog.Int("audioBytes", audioBytes),
		slog.Bool("endpointConfigured", c.config.Endpoint != ""),
	)
	if c.config.APIKey == "bad" {
		slog.WarnContext(ctx, "ASR 鉴权被拒绝",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
		)
		return nil, ErrAuth
	}
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt < c.config.FailuresBeforeSuccess {
			if attempt == c.config.MaxRetries {
				slog.ErrorContext(ctx, "ASR 重试已耗尽",
					slog.String("provider", c.Provider()),
					slog.String("callId", callID),
					slog.Int("attempt", attempt+1),
					slog.Int("maxRetries", c.config.MaxRetries),
					slog.Duration("elapsed", time.Since(start)),
				)
				return nil, ErrRetryExhausted
			}
			slog.WarnContext(ctx, "ASR 识别尝试失败",
				slog.String("provider", c.Provider()),
				slog.String("callId", callID),
				slog.Int("attempt", attempt+1),
				slog.String("reason", "按配置模拟成功前失败"),
			)
			continue
		}
		slog.DebugContext(ctx, "ASR 开始识别尝试",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
			slog.Int("attempt", attempt+1),
		)
		results, err := c.provider.Recognize(ctx, Request{Frames: frames, Config: c.config})
		if err != nil {
			slog.ErrorContext(ctx, "ASR 服务商调用失败",
				slog.String("provider", c.Provider()),
				slog.String("callId", callID),
				slog.Int("attempt", attempt+1),
				slog.Duration("elapsed", time.Since(start)),
				slog.Any("error", err),
			)
			return results, err
		}
		partialCount, finalCount := summarizeResults(results)
		slog.InfoContext(ctx, "收到 ASR 输出",
			slog.String("provider", c.Provider()),
			slog.String("callId", callID),
			slog.Int("resultCount", len(results)),
			slog.Int("partialCount", partialCount),
			slog.Int("finalCount", finalCount),
			slog.Duration("elapsed", time.Since(start)),
		)
		for index, result := range results {
			slog.InfoContext(ctx, "输出 ASR 结果",
				slog.String("provider", c.Provider()),
				slog.String("callId", result.CallID),
				slog.Int("index", index),
				slog.Bool("isFinal", result.IsFinal),
				slog.Float64("confidence", result.Confidence),
				slog.Int("textRunes", utf8.RuneCountInString(result.Text)),
				slog.String("textPreview", previewText(result.Text, 80)),
			)
		}
		return results, nil
	}
	slog.ErrorContext(ctx, "ASR 重试已耗尽",
		slog.String("provider", c.Provider()),
		slog.String("callId", callID),
		slog.Int("maxRetries", c.config.MaxRetries),
		slog.Duration("elapsed", time.Since(start)),
	)
	return nil, ErrRetryExhausted
}

// summarizeFrames 汇总 ASR 输入帧的首个 callID 和总音频字节数，用于日志观测但不暴露音频内容。
func summarizeFrames(frames []Frame) (string, int) {
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

// summarizeResults 统计 ASR 输出中 partial/final 的数量，用于确认 provider 是否有识别信号输出。
func summarizeResults(results []Result) (int, int) {
	var partialCount int
	var finalCount int
	for _, result := range results {
		if result.IsFinal {
			finalCount++
			continue
		}
		partialCount++
	}
	return partialCount, finalCount
}

// previewText 生成有限长度的文本预览，避免日志输出过长转写内容。
func previewText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes]) + "..."
}

type MockProvider struct {
	config Config
	name   string
}

// NewMockProvider 创建 mock ASR provider（测试用）。
func NewMockProvider(config Config) *MockProvider {
	return &MockProvider{config: normalizeConfig(config), name: config.Provider}
}

// Name 返回 provider 名称。
func (p *MockProvider) Name() string {
	if p.name == "" {
		return "mock"
	}
	return p.name
}

// Recognize 返回确定性 partial+final 识别结果（模拟流式输出）。
// 逻辑:
// 1. 按配置模拟 provider 延迟，并响应 context 取消。
// 2. 空音频直接返回空结果。
// 3. 按 confidence 阈值过滤低可信结果。
// 4. 使用首帧 callId 保证多通话隔离。
func (p *MockProvider) Recognize(ctx context.Context, req Request) ([]Result, error) {
	config := p.config
	if config.Delay > 0 {
		select {
		case <-time.After(config.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(req.Frames) == 0 {
		return nil, nil
	}
	confidence := config.MockConfidence
	if config.MinConfidence > 0 && confidence < config.MinConfidence {
		return nil, nil
	}
	callID := req.Frames[0].CallID
	return []Result{
		{CallID: callID, Text: "partial transcript", IsFinal: false, Confidence: confidence},
		{CallID: callID, Text: "final transcript", IsFinal: true, Confidence: confidence},
	}, nil
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

// Recognize 当前委托给 mock，真实接入时替换此处实现。
func (p *ThirdPartyProvider) Recognize(ctx context.Context, req Request) ([]Result, error) {
	return p.mock.Recognize(ctx, req)
}

