//  腾讯云语音合成(TTS)实现：文本→ WAV/PCM合成→ 返回音频数据
package tts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

const (
	tencentTTSDefaultEndpoint = "https://tts.tencentcloudapi.com"
	tencentTTSService         = "tts"
	tencentTTSAction          = "TextToVoice"
	tencentTTSVersion         = "2019-08-23"
	tencentTTSAlgorithm       = "TC3-HMAC-SHA256"
	tencentTTSContentType     = "application/json; charset=utf-8"
)

type TencentProvider struct {
	config Config
	client *http.Client
	now    func() time.Time
}

type tencentTTSConfig struct {
	SecretID        string
	SecretKey       string
	Endpoint        string
	Region          string
	VoiceType       string
	Codec           string
	SampleRate      int
	Speed           string
	Volume          string
	PrimaryLanguage int
}

type tencentTTSRequest struct {
	Text            string   `json:"Text"`
	SessionID       string   `json:"SessionId"`
	VoiceType       int      `json:"VoiceType,omitempty"`
	SampleRate      int      `json:"SampleRate,omitempty"`
	Codec           string   `json:"Codec,omitempty"`
	Volume          *float64 `json:"Volume,omitempty"`
	Speed           *float64 `json:"Speed,omitempty"`
	ProjectID       int      `json:"ProjectId"`
	ModelType       int      `json:"ModelType"`
	PrimaryLanguage int      `json:"PrimaryLanguage,omitempty"`
}

type tencentTTSResponse struct {
	Response struct {
		Audio     string           `json:"Audio"`
		SessionID string           `json:"SessionId"`
		RequestID string           `json:"RequestId"`
		Error     *tencentTTSError `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentTTSError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// NewTencentProvider 创建腾讯云 TTS provider。
func NewTencentProvider(config Config) *TencentProvider {
	return &TencentProvider{
		config: config,
		client: http.DefaultClient,
		now:    time.Now,
	}
}

// Name 返回 provider 名称。
func (p *TencentProvider) Name() string {
	if p.config.Provider == "" {
		return "tencent-tts"
	}
	return p.config.Provider
}

// Synthesize 调用腾讯云基础语音合成 API，并把 base64 音频解码成 TTSChunk。
// 逻辑:
// 1. 从 Config.Params/Secrets 解析腾讯云密钥、音色、格式、采样率等参数。
// 2. 每个文本分段单独调用 TextToVoice，避免超过腾讯单次文本长度限制。
// 3. 使用 TC3-HMAC-SHA256 签名 POST JSON 请求。
// 4. 解码响应中的 base64 音频，按原有 Chunk 结构返回给上层。
func (p *TencentProvider) Synthesize(ctx context.Context, req Request) ([]Chunk, error) {
	config := buildTencentTTSConfig(req.Config, req.Options)
	if config.SecretID == "" || config.SecretKey == "" {
		return nil, errors.New("tencent tts missing secretId/secretKey")
	}
	if len(req.Segments) == 0 && strings.TrimSpace(req.Text) != "" {
		req.Segments = []string{strings.TrimSpace(req.Text)}
	}

	chunks := make([]Chunk, 0, len(req.Segments))
	for index, segment := range req.Segments {
		if err := ctx.Err(); err != nil {
			return chunks, err
		}
		audio, sessionID, err := p.synthesizeSegment(ctx, config, req, segment, index)
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, Chunk{
			TTSChunk: model.TTSChunk{
				CallID:      req.Options.CallID,
				UtteranceID: req.Options.UtteranceID,
				Audio:       audio,
				Format:      config.Codec,
				SampleRate:  config.SampleRate,
				Sequence:    index,
				IsLast:      index == len(req.Segments)-1,
			},
			Text:     segment,
			Provider: p.Name(),
			Voice:    firstNonEmpty(req.Options.Voice, config.VoiceType),
			Language: req.Options.Language,
			Speed:    req.Options.Speed,
			Volume:   req.Options.Volume,
		})
		_ = sessionID
	}
	return chunks, nil
}

// synthesizeSegment 合成单个文本分段并返回原始音频字节。
func (p *TencentProvider) synthesizeSegment(ctx context.Context, config tencentTTSConfig, req Request, text string, index int) ([]byte, string, error) {
	endpoint, err := parseTencentTTSEndpoint(config.Endpoint)
	if err != nil {
		return nil, "", err
	}
	voiceType, err := parseOptionalInt(config.VoiceType)
	if err != nil {
		return nil, "", fmt.Errorf("tencent tts voiceType: %w", err)
	}
	body := tencentTTSRequest{
		Text:            text,
		SessionID:       tencentTTSSessionID(req.Options.CallID, req.Options.UtteranceID, index),
		VoiceType:       voiceType,
		SampleRate:      config.SampleRate,
		Codec:           config.Codec,
		ProjectID:       0,
		ModelType:       1,
		PrimaryLanguage: config.PrimaryLanguage,
	}
	if volume, ok, err := parseOptionalFloat(config.Volume); err != nil {
		return nil, "", fmt.Errorf("tencent tts volume: %w", err)
	} else if ok {
		body.Volume = &volume
	}
	if speed, ok, err := parseOptionalFloat(config.Speed); err != nil {
		return nil, "", fmt.Errorf("tencent tts speed: %w", err)
	} else if ok {
		body.Speed = &speed
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	slog.InfoContext(ctx, "腾讯 TTS 请求开始",
		slog.String("provider", p.Name()),
		slog.String("callId", req.Options.CallID),
		slog.String("utteranceId", req.Options.UtteranceID),
		slog.Int("segmentIndex", index),
		slog.String("host", endpoint.Host),
		slog.String("sessionId", body.SessionID),
		slog.Int("voiceType", body.VoiceType),
		slog.String("codec", body.Codec),
		slog.Int("sampleRate", body.SampleRate),
		slog.Int("textRunes", utf8.RuneCountInString(text)),
		slog.String("textPreview", previewText(text, 80)),
	)
	applyTencentCloudV3Headers(httpReq, config, endpoint.Host, payload, p.now())
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("tencent tts http status=%d body=%s", resp.StatusCode, truncateTencentTTSBody(raw, 300))
	}
	var parsed tencentTTSResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", err
	}
	if parsed.Response.Error != nil {
		return nil, "", fmt.Errorf("tencent tts error code=%s message=%s", parsed.Response.Error.Code, parsed.Response.Error.Message)
	}
	audio, err := base64.StdEncoding.DecodeString(parsed.Response.Audio)
	if err != nil {
		return nil, "", err
	}
	slog.InfoContext(ctx, "腾讯 TTS 响应已解析",
		slog.String("provider", p.Name()),
		slog.String("callId", req.Options.CallID),
		slog.String("utteranceId", req.Options.UtteranceID),
		slog.Int("segmentIndex", index),
		slog.String("sessionId", parsed.Response.SessionID),
		slog.String("codec", config.Codec),
		slog.Int("sampleRate", config.SampleRate),
		slog.Int("audioBytes", len(audio)),
	)
	return audio, parsed.Response.SessionID, nil
}

// buildTencentTTSConfig 从通用 TTS Config 中提取腾讯云语音合成参数。
func buildTencentTTSConfig(config Config, options Options) tencentTTSConfig {
	params := config.Params
	codec := normalizeTencentTTSCodec(firstNonEmpty(params["codec"], options.Format, config.DefaultFormat, "wav"))
	sampleRate := firstNonZero(parseInt(params["sampleRate"]), options.SampleRate, config.DefaultRate, 16000)
	return tencentTTSConfig{
		SecretID:        firstNonEmpty(config.Secrets["secretId"], config.Secrets["secretID"]),
		SecretKey:       firstNonEmpty(config.Secrets["secretKey"], config.Secrets["secret"]),
		Endpoint:        firstNonEmpty(config.Endpoint, params["endpoint"], tencentTTSDefaultEndpoint),
		Region:          params["region"],
		VoiceType:       firstNonEmpty(options.Voice, params["voiceType"], config.DefaultVoice),
		Codec:           codec,
		SampleRate:      sampleRate,
		Speed:           params["speed"],
		Volume:          params["volume"],
		PrimaryLanguage: tencentPrimaryLanguage(firstNonEmpty(params["primaryLanguage"], params["language"], options.Language, config.DefaultLanguage)),
	}
}

// applyTencentCloudV3Headers 给腾讯云 API 请求设置公共头和 Authorization。
func applyTencentCloudV3Headers(req *http.Request, config tencentTTSConfig, host string, payload []byte, now time.Time) {
	timestamp := now.Unix()
	authorization := tencentCloudV3Authorization(tencentCloudV3Input{
		SecretID:  config.SecretID,
		SecretKey: config.SecretKey,
		Service:   tencentTTSService,
		Host:      host,
		Action:    tencentTTSAction,
		Payload:   payload,
		Timestamp: timestamp,
	})
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", tencentTTSContentType)
	req.Header.Set("Host", host)
	req.Host = host
	req.Header.Set("X-TC-Action", tencentTTSAction)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", tencentTTSVersion)
	if config.Region != "" {
		req.Header.Set("X-TC-Region", config.Region)
	}
}

type tencentCloudV3Input struct {
	SecretID  string
	SecretKey string
	Service   string
	Host      string
	Action    string
	Payload   []byte
	Timestamp int64
}

// tencentCloudV3Authorization 计算腾讯云 API 3.0 签名 Authorization。
func tencentCloudV3Authorization(input tencentCloudV3Input) string {
	date := time.Unix(input.Timestamp, 0).UTC().Format("2006-01-02")
	canonicalHeaders := "content-type:" + tencentTTSContentType + "\n" +
		"host:" + input.Host + "\n" +
		"x-tc-action:" + strings.ToLower(input.Action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	hashedPayload := sha256Hex(input.Payload)
	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")
	credentialScope := date + "/" + input.Service + "/tc3_request"
	stringToSign := strings.Join([]string{
		tencentTTSAlgorithm,
		strconv.FormatInt(input.Timestamp, 10),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	secretDate := hmacSHA256([]byte("TC3"+input.SecretKey), date)
	secretService := hmacSHA256(secretDate, input.Service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	return tencentTTSAlgorithm + " Credential=" + input.SecretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

// parseTencentTTSEndpoint 解析 TTS endpoint。
func parseTencentTTSEndpoint(raw string) (*url.URL, error) {
	if raw == "" {
		raw = tencentTTSDefaultEndpoint
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return nil, errors.New("tencent tts endpoint missing host")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed, nil
}

// sha256Hex 计算 SHA256 十六进制小写摘要。
func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// hmacSHA256 计算 HMAC-SHA256 二进制摘要。
func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

// tencentTTSSessionID 生成腾讯 TTS SessionId。
func tencentTTSSessionID(callID, utteranceID string, index int) string {
	prefix := firstNonEmpty(callID, "call")
	if utteranceID != "" {
		prefix += "-" + utteranceID
	}
	return fmt.Sprintf("%s-%d-%d", prefix, index, time.Now().UnixNano())
}

// normalizeTencentTTSCodec 归一化腾讯 TTS 音频格式。
func normalizeTencentTTSCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "", "wav":
		return "wav"
	case "mp3":
		return "mp3"
	case "pcm":
		return "pcm"
	default:
		return strings.TrimSpace(codec)
	}
}

// tencentPrimaryLanguage 将语言配置映射为腾讯 PrimaryLanguage。
func tencentPrimaryLanguage(language string) int {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "2", "en", "en-us", "en_us":
		return 2
	default:
		return 1
	}
}

// parseOptionalInt 解析可选整数，空值返回 0。
func parseOptionalInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

// parseOptionalFloat 解析可选浮点数。
func parseOptionalFloat(value string) (float64, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, true, err
}

// parseInt 解析整数，失败时返回 0。
func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// firstNonZero 返回第一个非零整数。
func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// truncateTencentTTSBody 截断错误响应体，避免日志过大。
func truncateTencentTTSBody(body []byte, limit int) string {
	text := string(body)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

