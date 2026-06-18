//  腾讯云机器翻译(TMT)实现：文本翻译API调用→ 快翻草稿
package tmt

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
)

const (
	tencentTMTDefaultEndpoint  = "https://tmt.tencentcloudapi.com"
	tencentTMTDefaultRegion    = "ap-guangzhou"
	tencentTMTService          = "tmt"
	tencentTMTAction           = "TextTranslate"
	tencentTMTVersion          = "2018-03-21"
	tencentTMTAlgorithm        = "TC3-HMAC-SHA256"
	tencentTMTContentType      = "application/json; charset=utf-8"
	tencentTMTPreviewBodyRunes = 300
)

type TencentTMTProvider struct {
	config Config
	client *http.Client
	now    func() time.Time
}

type tencentTMTConfig struct {
	SecretID  string
	SecretKey string
	Endpoint  string
	Region    string
	ProjectID int
}

type tencentTMTRequest struct {
	SourceText string `json:"SourceText"`
	Source     string `json:"Source"`
	Target     string `json:"Target"`
	ProjectID  int    `json:"ProjectId"`
}

type tencentTMTResponse struct {
	Response struct {
		TargetText string           `json:"TargetText"`
		Source     string           `json:"Source"`
		Target     string           `json:"Target"`
		RequestID  string           `json:"RequestId"`
		Error      *tencentTMTError `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentTMTError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// NewTencentTMTProvider 创建腾讯机器翻译 provider。
func NewTencentTMTProvider(config Config) *TencentTMTProvider {
	return &TencentTMTProvider{
		config: config,
		client: http.DefaultClient,
		now:    time.Now,
	}
}

func (p *TencentTMTProvider) Name() string {
	if p.config.Provider == "" {
		return "tencent-tmt"
	}
	return p.config.Provider
}

// Translate 调用腾讯云 TextTranslate，输入英文 ASR 文本并返回中文译文。
func (p *TencentTMTProvider) Translate(ctx context.Context, req Request) (Result, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, nil
	}
	config := buildTencentTMTConfig(p.config)
	if config.SecretID == "" || config.SecretKey == "" {
		return Result{}, errors.New("tencent tmt missing secretId/secretKey")
	}
	endpoint, err := parseTencentTMTEndpoint(config.Endpoint)
	if err != nil {
		return Result{}, err
	}
	source := tencentTMTLanguage(req.SourceLang, p.config.Params["source"], "en")
	target := tencentTMTLanguage(req.TargetLang, p.config.Params["target"], "zh")
	body := tencentTMTRequest{
		SourceText: text,
		Source:     source,
		Target:     target,
		ProjectID:  config.ProjectID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	applyTencentTMTHeaders(httpReq, config, endpoint.Host, payload, p.now())
	slog.InfoContext(ctx, "腾讯 TMT 请求开始",
		slog.String("provider", p.Name()),
		slog.String("callId", req.CallID),
		slog.String("utteranceId", req.UtteranceID),
		slog.String("host", endpoint.Host),
		slog.String("source", source),
		slog.String("target", target),
		slog.String("region", config.Region),
		slog.Int("projectId", config.ProjectID),
		slog.Int("textRunes", utf8.RuneCountInString(text)),
		slog.String("textPreview", previewTMTText(text, 80)),
	)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("tencent tmt http status=%d body=%s", resp.StatusCode, previewTMTBody(raw))
	}
	var parsed tencentTMTResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, err
	}
	if parsed.Response.Error != nil {
		return Result{}, fmt.Errorf("tencent tmt error code=%s message=%s", parsed.Response.Error.Code, parsed.Response.Error.Message)
	}
	translation := strings.TrimSpace(parsed.Response.TargetText)
	slog.InfoContext(ctx, "腾讯 TMT 响应已解析",
		slog.String("provider", p.Name()),
		slog.String("callId", req.CallID),
		slog.String("utteranceId", req.UtteranceID),
		slog.String("requestId", parsed.Response.RequestID),
		slog.Int("textRunes", utf8.RuneCountInString(translation)),
		slog.String("textPreview", previewTMTText(translation, 80)),
	)
	return Result{Text: translation}, nil
}

func buildTencentTMTConfig(config Config) tencentTMTConfig {
	params := config.Params
	return tencentTMTConfig{
		SecretID:  firstTMTValue(config.Secrets["secretId"], config.Secrets["secretID"], params["secretId"], params["secretID"]),
		SecretKey: firstTMTValue(config.Secrets["secretKey"], config.Secrets["secret"], params["secretKey"], params["secret"]),
		Endpoint:  firstTMTValue(config.Endpoint, params["endpoint"], tencentTMTDefaultEndpoint),
		Region:    firstTMTValue(config.Region, params["region"], tencentTMTDefaultRegion),
		ProjectID: firstTMTInt(parseTMTInt(params["projectId"]), parseTMTInt(params["projectID"]), parseTMTInt(params["project_id"]), 0),
	}
}

func applyTencentTMTHeaders(req *http.Request, config tencentTMTConfig, host string, payload []byte, now time.Time) {
	timestamp := now.Unix()
	authorization := tencentTMTAuthorization(tencentTMTSignInput{
		SecretID:  config.SecretID,
		SecretKey: config.SecretKey,
		Service:   tencentTMTService,
		Host:      host,
		Action:    tencentTMTAction,
		Payload:   payload,
		Timestamp: timestamp,
	})
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", tencentTMTContentType)
	req.Header.Set("Host", host)
	req.Host = host
	req.Header.Set("X-TC-Action", tencentTMTAction)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", tencentTMTVersion)
	if config.Region != "" {
		req.Header.Set("X-TC-Region", config.Region)
	}
}

type tencentTMTSignInput struct {
	SecretID  string
	SecretKey string
	Service   string
	Host      string
	Action    string
	Payload   []byte
	Timestamp int64
}

func tencentTMTAuthorization(input tencentTMTSignInput) string {
	date := time.Unix(input.Timestamp, 0).UTC().Format("2006-01-02")
	canonicalHeaders := "content-type:" + tencentTMTContentType + "\n" +
		"host:" + input.Host + "\n" +
		"x-tc-action:" + strings.ToLower(input.Action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		sha256HexTMT(input.Payload),
	}, "\n")
	credentialScope := date + "/" + input.Service + "/tc3_request"
	stringToSign := strings.Join([]string{
		tencentTMTAlgorithm,
		strconv.FormatInt(input.Timestamp, 10),
		credentialScope,
		sha256HexTMT([]byte(canonicalRequest)),
	}, "\n")
	secretDate := hmacSHA256TMT([]byte("TC3"+input.SecretKey), date)
	secretService := hmacSHA256TMT(secretDate, input.Service)
	secretSigning := hmacSHA256TMT(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256TMT(secretSigning, stringToSign))
	return tencentTMTAlgorithm + " Credential=" + input.SecretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func parseTencentTMTEndpoint(raw string) (*url.URL, error) {
	if raw == "" {
		raw = tencentTMTDefaultEndpoint
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
		return nil, errors.New("tencent tmt endpoint missing host")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed, nil
}

func tencentTMTLanguage(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstTMTValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseTMTInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func firstTMTInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func sha256HexTMT(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256TMT(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func previewTMTBody(raw []byte) string {
	runes := []rune(strings.TrimSpace(string(raw)))
	if len(runes) > tencentTMTPreviewBodyRunes {
		return string(runes[:tencentTMTPreviewBodyRunes]) + "..."
	}
	return string(runes)
}

func previewTMTText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

