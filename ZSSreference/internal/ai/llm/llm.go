// Package llm 提供 OpenAI-compatible/DeepSeek 翻译校准能力。
//
// 主链路在 ASR final 后调用 LLM，输入当前英文句子和前几句 ASR final 原文，
// 输出最终中文译文供前端覆盖 TMT 草稿并触发自动配音。
package llm

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
	Quality         bool              // true=ASR final 后的高确定性翻译
	SourceContext   []string          // 前几句 ASR final 原文，供模型理解上下文但不重译
	Context         []string          // 兼容旧调用：最近若干句目标语译文
	Glossary        map[string]string // 可选术语表（源词->译法）
	FastTranslation string            // 可选机器翻译草稿
}

// Result 为翻译结果。Terms 为本次重译产生的术语补充，调用方应回写会话术语表。
type Result struct {
	Text  string
	Terms map[string]string
}

type VocabularyText struct {
	UtteranceID string
	Text        string
}

type VocabularyRequest struct {
	TaskID    string
	SessionID string
	TenantID  string
	UserID    string
	Texts     []VocabularyText
	MaxWords  int
}

type VocabularyEntry struct {
	Word               string
	Lemma              string
	Phonetic           string
	PartOfSpeech       string
	MeaningZH          string
	ExampleEN          string
	ExampleZH          string
	Occurrences        int
	Difficulty         string
	SourceUtteranceIDs []string
}

type VocabularyResult struct {
	Entries         []VocabularyEntry
	RawRequestJSON  string
	RawResponseJSON string
}

// Provider 为具体翻译服务商需要实现的接口。
type Provider interface {
	Name() string
	Translate(ctx context.Context, req Request) (Result, error)
}

type VocabularyProvider interface {
	ExtractVocabulary(ctx context.Context, req VocabularyRequest) (VocabularyResult, error)
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
	// DeepSeek（OpenAI 兼容 Chat Completions 接口）。
	RegisterProvider("deepseek", func(config Config) Provider {
		return NewDeepSeekProvider(config)
	})
	// 通用 OpenAI-compatible Chat Completions 接口。
	RegisterProvider("openai-compatible", func(config Config) Provider {
		return NewOpenAICompatibleProvider(config)
	})
	RegisterProvider("openai", func(config Config) Provider {
		return NewOpenAICompatibleProvider(config)
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
	if config.Provider == "deepseek" && config.Model == "" {
		config.Model = "deepseek-chat"
	}
	if config.Provider == "deepseek" && config.Endpoint == "" {
		config.Endpoint = "https://api.deepseek.com/v1/chat/completions"
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

// ExtractVocabulary 使用大模型从会话英文文本中总结学习词条。
func (c *Client) ExtractVocabulary(ctx context.Context, req VocabularyRequest) (VocabularyResult, error) {
	if c.config.APIKey == "bad" {
		return VocabularyResult{}, ErrAuth
	}
	provider, ok := c.provider.(VocabularyProvider)
	if !ok {
		return VocabularyResult{}, ErrUnsupported
	}
	if req.MaxWords <= 0 {
		req.MaxWords = 30
	}
	start := time.Now()
	callCtx := ctx
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}
	result, err := provider.ExtractVocabulary(callCtx, req)
	if err != nil {
		slog.WarnContext(ctx, "单词本总结失败",
			slog.String("provider", c.Provider()),
			slog.String("taskId", req.TaskID),
			slog.String("sessionId", req.SessionID),
			slog.Duration("elapsed", time.Since(start)),
			slog.Any("error", err),
		)
		return result, err
	}
	result.Entries = sanitizeVocabularyEntries(result.Entries, req.MaxWords)
	slog.InfoContext(ctx, "单词本总结完成",
		slog.String("provider", c.Provider()),
		slog.String("taskId", req.TaskID),
		slog.String("sessionId", req.SessionID),
		slog.Int("entries", len(result.Entries)),
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

func (p *MockProvider) ExtractVocabulary(ctx context.Context, req VocabularyRequest) (VocabularyResult, error) {
	if err := ctx.Err(); err != nil {
		return VocabularyResult{}, err
	}
	if len(req.Texts) == 0 {
		return VocabularyResult{RawRequestJSON: `{"provider":"mock"}`, RawResponseJSON: `{"entries":[]}`}, nil
	}
	word := "architecture"
	example := strings.TrimSpace(req.Texts[0].Text)
	if example == "" {
		example = "We use a transformer architecture."
	}
	return VocabularyResult{
		Entries: []VocabularyEntry{{
			Word:               word,
			Lemma:              word,
			Phonetic:           "/ˈɑːrkɪtektʃər/",
			PartOfSpeech:       "noun",
			MeaningZH:          "架构；体系结构",
			ExampleEN:          example,
			ExampleZH:          "我们使用 Transformer 架构。",
			Occurrences:        1,
			Difficulty:         "B2",
			SourceUtteranceIDs: []string{req.Texts[0].UtteranceID},
		}},
		RawRequestJSON:  `{"provider":"mock"}`,
		RawResponseJSON: `{"entries":[{"word":"architecture"}]}`,
	}, nil
}

func sanitizeVocabularyEntries(entries []VocabularyEntry, maxWords int) []VocabularyEntry {
	if maxWords <= 0 {
		maxWords = 30
	}
	seen := map[string]bool{}
	out := make([]VocabularyEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Word = strings.TrimSpace(entry.Word)
		entry.Lemma = strings.TrimSpace(entry.Lemma)
		key := strings.ToLower(firstVocabularyValue(entry.Lemma, entry.Word))
		if entry.Word == "" || key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if entry.Occurrences < 0 {
			entry.Occurrences = 0
		}
		out = append(out, entry)
		if len(out) >= maxWords {
			break
		}
	}
	return out
}

func firstVocabularyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
