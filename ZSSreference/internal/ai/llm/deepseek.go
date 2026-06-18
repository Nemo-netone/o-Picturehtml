// DeepSeek Flash翻译：OpenAI兼容接口→ 注入上下文+术语表→ 纠错输出
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAICompatibleProvider 通过 OpenAI 兼容的 Chat Completions 接口完成翻译。
// Quality=true 时注入前几句 ASR final 原文，要求返回 JSON {translation, terms}。
type OpenAICompatibleProvider struct {
	config Config
	http   *http.Client
	name   string
}

// DeepSeekProvider 保留旧类型名，底层走 OpenAI-compatible Chat Completions。
type DeepSeekProvider = OpenAICompatibleProvider

// NewOpenAICompatibleProvider 创建通用 OpenAI-compatible 翻译 provider。
func NewOpenAICompatibleProvider(config Config) *OpenAICompatibleProvider {
	name := strings.TrimSpace(config.Provider)
	if name == "" {
		name = "openai-compatible"
	}
	return &OpenAICompatibleProvider{config: config, http: &http.Client{}, name: name}
}

// NewDeepSeekProvider 创建 DeepSeek 翻译 provider。
func NewDeepSeekProvider(config Config) *OpenAICompatibleProvider {
	if strings.TrimSpace(config.Provider) == "" {
		config.Provider = "deepseek"
	}
	return NewOpenAICompatibleProvider(config)
}

// Name 返回 provider 名称。
func (p *OpenAICompatibleProvider) Name() string { return p.name }

type deepseekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepseekRequest struct {
	Model          string            `json:"model"`
	Messages       []deepseekMessage `json:"messages"`
	Temperature    float64           `json:"temperature"`
	Stream         bool              `json:"stream"`
	ResponseFormat *deepseekFormat   `json:"response_format,omitempty"`
}

type deepseekFormat struct {
	Type string `json:"type"`
}

type deepseekResponse struct {
	Choices []struct {
		Message deepseekMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type vocabularyContent struct {
	Entries []vocabularyEntryContent `json:"entries"`
}

type vocabularyEntryContent struct {
	Word               string   `json:"word"`
	Lemma              string   `json:"lemma"`
	Phonetic           string   `json:"phonetic"`
	PartOfSpeech       string   `json:"partOfSpeech"`
	MeaningZH          string   `json:"meaningZh"`
	ExampleEN          string   `json:"exampleEn"`
	ExampleZH          string   `json:"exampleZh"`
	Occurrences        int      `json:"occurrences"`
	Difficulty         string   `json:"difficulty"`
	SourceUtteranceIDs []string `json:"sourceUtteranceIds"`
}

// Translate 调用 OpenAI-compatible Chat Completions 完成一次翻译。
func (p *OpenAICompatibleProvider) Translate(ctx context.Context, req Request) (Result, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, nil
	}
	apiKey := firstOpenAIValue(p.config.APIKey, p.config.Secrets["apiKey"], p.config.Secrets["api_key"], p.config.Secrets["token"])
	if apiKey == "" {
		return Result{}, ErrAuth
	}
	endpoint := normalizeOpenAIChatEndpoint(p.config.Endpoint)
	if endpoint == "" {
		return Result{}, fmt.Errorf("%s endpoint is empty", p.Name())
	}
	model := strings.TrimSpace(p.config.Model)
	if model == "" {
		return Result{}, fmt.Errorf("%s model is empty", p.Name())
	}

	payload := deepseekRequest{
		Model:       model,
		Temperature: 0,
		Stream:      false,
	}
	if req.Quality {
		payload.Messages = qualityMessages(req)
		payload.ResponseFormat = &deepseekFormat{Type: "json_object"}
	} else {
		payload.Messages = fastMessages(req)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Result{}, ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("%s http %d: %s", p.Name(), resp.StatusCode, previewBody(raw))
	}

	var parsed deepseekResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, fmt.Errorf("decode %s response: %w", p.Name(), err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Result{}, fmt.Errorf("%s error: %s", p.Name(), parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("%s empty choices", p.Name())
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)

	if req.Quality {
		return parseQualityContent(content), nil
	}
	return Result{Text: content}, nil
}

// ExtractVocabulary 调用 OpenAI-compatible Chat Completions 从会话文本总结英文学习词条。
func (p *OpenAICompatibleProvider) ExtractVocabulary(ctx context.Context, req VocabularyRequest) (VocabularyResult, error) {
	apiKey := firstOpenAIValue(p.config.APIKey, p.config.Secrets["apiKey"], p.config.Secrets["api_key"], p.config.Secrets["token"])
	if apiKey == "" {
		return VocabularyResult{}, ErrAuth
	}
	endpoint := normalizeOpenAIChatEndpoint(p.config.Endpoint)
	if endpoint == "" {
		return VocabularyResult{}, fmt.Errorf("%s endpoint is empty", p.Name())
	}
	model := strings.TrimSpace(p.config.Model)
	if model == "" {
		return VocabularyResult{}, fmt.Errorf("%s model is empty", p.Name())
	}
	if req.MaxWords <= 0 {
		req.MaxWords = 30
	}

	payload := deepseekRequest{
		Model:          model,
		Temperature:    0,
		Stream:         false,
		ResponseFormat: &deepseekFormat{Type: "json_object"},
		Messages:       vocabularyMessages(req),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return VocabularyResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return VocabularyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return VocabularyResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VocabularyResult{}, err
	}
	rawRequestJSON := string(body)
	rawResponseJSON := string(raw)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, fmt.Errorf("%s http %d: %s", p.Name(), resp.StatusCode, previewBody(raw))
	}

	var parsed deepseekResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, fmt.Errorf("decode %s response: %w", p.Name(), err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, fmt.Errorf("%s error: %s", p.Name(), parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, fmt.Errorf("%s empty choices", p.Name())
	}
	entries, err := parseVocabularyContent(parsed.Choices[0].Message.Content)
	if err != nil {
		return VocabularyResult{RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, err
	}
	return VocabularyResult{Entries: entries, RawRequestJSON: rawRequestJSON, RawResponseJSON: rawResponseJSON}, nil
}

// fastMessages 构造快翻提示词：纯文本、最低延迟。
func fastMessages(req Request) []deepseekMessage {
	source := displayLanguage(defaultLanguage(req.SourceLang, "en"))
	target := displayLanguage(defaultLanguage(req.TargetLang, "zh"))
	system := "You are a professional real-time interpreter. Translate the " + source + " subtitle fragment into natural, concise " + target + ". Output ONLY the " + target + " translation with no quotes, no explanation, and no source-language text."
	return []deepseekMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: req.Text},
	}
}

// qualityMessages 构造重译提示词：注入上下文 + 术语表，要求 JSON 输出以便提取术语。
func qualityMessages(req Request) []deepseekMessage {
	source := displayLanguage(defaultLanguage(req.SourceLang, "en"))
	target := displayLanguage(defaultLanguage(req.TargetLang, "zh"))
	var b strings.Builder
	b.WriteString("You are a professional simultaneous interpreter translating a ")
	b.WriteString(source)
	b.WriteString(" talk into natural, fluent ")
	b.WriteString(target)
	b.WriteString(" for live subtitles.\n")
	b.WriteString("Translate only the NEW ")
	b.WriteString(source)
	b.WriteString(" sentence into ")
	b.WriteString(target)
	b.WriteString(". Use previous ASR final transcripts only to understand context. Do not translate previous context lines.\n")
	if len(req.SourceContext) > 0 {
		b.WriteString("\nPrevious ASR final transcripts (context only, do NOT translate them):\n")
		for _, line := range req.SourceContext {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if len(req.Glossary) > 0 {
		b.WriteString("\nGlossary (use these ")
		b.WriteString(target)
		b.WriteString(" translations consistently):\n")
		for en, zh := range req.Glossary {
			b.WriteString("- ")
			b.WriteString(en)
			b.WriteString(" => ")
			b.WriteString(zh)
			b.WriteString("\n")
		}
	}
	if len(req.Context) > 0 {
		b.WriteString("\nPreceding translated subtitles (compatibility context only, do NOT retranslate them):\n")
		for _, line := range req.Context {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(req.FastTranslation) != "" {
		b.WriteString("\nCurrent machine translation draft (revise only if needed):\n")
		b.WriteString(req.FastTranslation)
		b.WriteString("\n")
	}
	b.WriteString("\nReturn a strict JSON object: {\"translation\": \"<")
	b.WriteString(target)
	b.WriteString(" translation>\", \"terms\": {\"<source term>\": \"<")
	b.WriteString(target)
	b.WriteString(">\"}}. ")
	b.WriteString("Put only important domain/technical terms from THIS sentence into \"terms\" (may be empty). Output JSON only.")

	return []deepseekMessage{
		{Role: "system", Content: b.String()},
		{Role: "user", Content: "NEW " + source + " sentence: " + req.Text},
	}
}

func vocabularyMessages(req VocabularyRequest) []deepseekMessage {
	var b strings.Builder
	b.WriteString("You are an English vocabulary teacher for Chinese learners. ")
	b.WriteString("Summarize useful English words or short phrases from the conversation transcript. ")
	b.WriteString("Ignore common function words and choose items with learning value. ")
	b.WriteString("Return a strict JSON object with key \"entries\". ")
	b.WriteString("Each entry must contain: word, lemma, phonetic, partOfSpeech, meaningZh, exampleEn, exampleZh, occurrences, difficulty, sourceUtteranceIds. ")
	b.WriteString("Use Chinese for meaningZh and exampleZh. ")
	b.WriteString("Return at most ")
	b.WriteString(fmt.Sprintf("%d", req.MaxWords))
	b.WriteString(" entries. If no useful English vocabulary exists, return {\"entries\":[]}.\n")
	b.WriteString("\nConversation transcript:\n")
	for _, text := range req.Texts {
		line := strings.TrimSpace(text.Text)
		if line == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(strings.TrimSpace(text.UtteranceID))
		b.WriteString("] ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return []deepseekMessage{
		{Role: "system", Content: b.String()},
		{Role: "user", Content: "Create the vocabulary JSON now."},
	}
}

func parseVocabularyContent(content string) ([]VocabularyEntry, error) {
	content = strings.TrimSpace(content)
	var parsed vocabularyContent
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("decode vocabulary json: %w", err)
	}
	out := make([]VocabularyEntry, 0, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		out = append(out, VocabularyEntry{
			Word:               strings.TrimSpace(entry.Word),
			Lemma:              strings.TrimSpace(entry.Lemma),
			Phonetic:           strings.TrimSpace(entry.Phonetic),
			PartOfSpeech:       strings.TrimSpace(entry.PartOfSpeech),
			MeaningZH:          strings.TrimSpace(entry.MeaningZH),
			ExampleEN:          strings.TrimSpace(entry.ExampleEN),
			ExampleZH:          strings.TrimSpace(entry.ExampleZH),
			Occurrences:        entry.Occurrences,
			Difficulty:         strings.TrimSpace(entry.Difficulty),
			SourceUtteranceIDs: compactStrings(entry.SourceUtteranceIDs),
		})
	}
	return out, nil
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func defaultLanguage(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func displayLanguage(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "zh", "zh-cn":
		return "Chinese"
	case "en", "en-us":
		return "English"
	case "id":
		return "Indonesian"
	case "fil":
		return "Filipino"
	case "th":
		return "Thai"
	case "pt":
		return "Portuguese"
	case "tr":
		return "Turkish"
	case "ar":
		return "Arabic"
	case "es":
		return "Spanish"
	case "hi":
		return "Hindi"
	case "fr":
		return "French"
	case "de":
		return "German"
	default:
		if strings.TrimSpace(code) == "" {
			return "English"
		}
		return strings.TrimSpace(code)
	}
}

type qualityPayload struct {
	Translation string            `json:"translation"`
	Terms       map[string]string `json:"terms"`
}

// parseQualityContent 解析重译返回的 JSON；解析失败时退化为把全文当作译文（保证不中断主链路）。
func parseQualityContent(content string) Result {
	cleaned := stripCodeFence(content)
	var payload qualityPayload
	if err := json.Unmarshal([]byte(cleaned), &payload); err == nil && strings.TrimSpace(payload.Translation) != "" {
		return Result{Text: strings.TrimSpace(payload.Translation), Terms: payload.Terms}
	}
	return Result{Text: content}
}

// stripCodeFence 去除模型可能包裹的 ```json ... ``` 代码块围栏。
func stripCodeFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

// previewBody 截断错误响应体，避免日志/错误过长。
func previewBody(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

func firstOpenAIValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeOpenAIChatEndpoint(endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return ""
	}
	if strings.Contains(endpoint, "/chat/completions") {
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/v1") {
		return endpoint + "/chat/completions"
	}
	return endpoint + "/v1/chat/completions"
}
