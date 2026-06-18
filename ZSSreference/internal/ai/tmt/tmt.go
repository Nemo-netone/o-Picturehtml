// Package tmt 提供机器翻译能力，结构对称于 internal/ai/asr 与 internal/ai/tts。
//
// 当前主链路只在 ASR final 后触发 TMT，TMT 结果作为低延迟中文草稿回传 api-server；
// DeepSeek/LLM 后续可基于 ASR final、TMT 草稿和上下文产出最终修订译文。
package tmt

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
	// ErrAuth 表示翻译服务鉴权失败。
	ErrAuth = errors.New("translate auth error")
	// ErrUnsupported 表示当前 provider 未实现真实翻译（占位实现）。
	ErrUnsupported = errors.New("translate provider unsupported")
)

// Config 为翻译客户端配置。APIKey 等敏感信息由服务端环境变量提供，不经浏览器。
type Config struct {
	Provider string
	APIKey   string
	Endpoint string
	Model    string
	Params   map[string]string
	Secrets  map[string]string
	Region   string
	Timeout  time.Duration
}

// Request 为一次翻译请求。
type Request struct {
	CallID          string
	UtteranceID     string
	Text            string            // 源文本（英文）
	SourceLang      string            // 例如 "en"
	TargetLang      string            // 例如 "zh"
	Quality         bool              // true 表示 ASR final 触发的高确定性翻译
	Context         []string          // 预留：最近若干句已锁定的目标语译文
	Glossary        map[string]string // 预留：会话级术语表（源词->译法）
	FastTranslation string            // 预留：已有草稿译文
}

// Result 为翻译结果。Terms 为本次重译产生的术语补充，调用方应回写会话术语表。
type Result struct {
	Text  string
	Terms map[string]string
}

// Provider 为具体翻译服务商需要实现的接口。
type Provider interface {
	Name() string
	Translate(ctx context.Context, req Request) (Result, error)
}

// ProviderFactory 按配置构造一个 Provider。
type ProviderFactory func(config Config) Provider

// Client 包装 Provider，统一做鉴权、超时、日志与降级。
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

// init 注册内置翻译 provider 工厂。
func init() {
	// mock 工厂（测试/无 key 时使用，确定性输出）。
	RegisterProvider("mock", func(config Config) Provider {
		return NewMockProvider(config)
	})
	// 腾讯机器翻译 TMT。
	RegisterProvider("tencent-tmt", func(config Config) Provider {
		return NewTencentTMTProvider(config)
	})
}

// NewClient 根据配置创建翻译客户端（从全局 registry 查找 provider）。
func NewClient(config Config) *Client {
	config = normalizeConfig(config)
	return &Client{config: config, provider: providerFor(config)}
}

// NewClientWithProvider 创建使用指定 provider 的翻译客户端（注入方式，用于测试）。
func NewClientWithProvider(config Config, provider Provider) *Client {
	config = normalizeConfig(config)
	if provider == nil {
		provider = providerFor(config)
	}
	return &Client{config: config, provider: provider}
}

// RegisterProvider 注册一个新的翻译 provider 工厂。
func RegisterProvider(name string, factory ProviderFactory) {
	if name == "" || factory == nil {
		return
	}
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.factories[name] = factory
}

// normalizeConfig 填充翻译配置默认值。
func normalizeConfig(config Config) Config {
	if config.Provider == "" {
		config.Provider = "mock"
	}
	if config.Timeout <= 0 {
		config.Timeout = 8 * time.Second
	}
	return config
}

// providerFor 按名称查找 provider 工厂，未找到时退回 mock。
func providerFor(config Config) Provider {
	providerRegistry.RLock()
	factory := providerRegistry.factories[config.Provider]
	providerRegistry.RUnlock()
	if factory != nil {
		return factory(config)
	}
	return NewMockProvider(config)
}

// Provider 返回当前 provider 名称。
func (c *Client) Provider() string {
	return c.provider.Name()
}

// Translate 执行翻译：鉴权 → 设置超时 → 调用 provider → 记录指标。
// 返回错误时调用方应做降级（保留上一帧或快翻结果），不得中断字幕主链路。
func (c *Client) Translate(ctx context.Context, req Request) (Result, error) {
	if c.config.APIKey == "bad" {
		return Result{}, ErrAuth
	}
	req = c.applyDefaults(req)
	mode := "fast"
	if req.Quality {
		mode = "quality"
	}
	start := time.Now()

	callCtx := ctx
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}

	result, err := c.provider.Translate(callCtx, req)
	if err != nil {
		slog.WarnContext(ctx, "翻译服务商调用失败",
			slog.String("provider", c.Provider()),
			slog.String("callId", req.CallID),
			slog.String("utteranceId", req.UtteranceID),
			slog.String("mode", mode),
			slog.Duration("elapsed", time.Since(start)),
			slog.Any("error", err),
		)
		return result, err
	}
	result.Text = strings.TrimSpace(result.Text)
	slog.InfoContext(ctx, "翻译完成",
		slog.String("provider", c.Provider()),
		slog.String("callId", req.CallID),
		slog.String("utteranceId", req.UtteranceID),
		slog.String("mode", mode),
		slog.Int("sourceRunes", utf8.RuneCountInString(req.Text)),
		slog.Int("targetRunes", utf8.RuneCountInString(result.Text)),
		slog.Int("newTerms", len(result.Terms)),
		slog.Duration("elapsed", time.Since(start)),
	)
	return result, nil
}

// applyDefaults 补齐请求语言方向默认值（en->zh）。
func (c *Client) applyDefaults(req Request) Request {
	if req.SourceLang == "" {
		req.SourceLang = "en"
	}
	if req.TargetLang == "" {
		req.TargetLang = "zh"
	}
	return req
}

// MockProvider 返回确定性译文，用于测试或无 key 环境，不发起任何网络请求。
type MockProvider struct {
	name string
}

// NewMockProvider 创建 mock 翻译 provider。
func NewMockProvider(config Config) *MockProvider {
	name := config.Provider
	if name == "" {
		name = "mock"
	}
	return &MockProvider{name: name}
}

// Name 返回 provider 名称。
func (p *MockProvider) Name() string {
	return p.name
}

// Translate 返回带标记的确定性译文，便于断言快翻/重译路径。
func (p *MockProvider) Translate(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	prefix := "[译]"
	if req.Quality {
		prefix = "[重译]"
	}
	return Result{Text: prefix + strings.TrimSpace(req.Text)}, nil
}
