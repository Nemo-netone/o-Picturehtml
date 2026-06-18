//  腾讯云实时ASR WebSocket实现：签名鉴权→ 建立长连接→ 持续写入PCM→ 消费识别结果
package asr

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	xwebsocket "golang.org/x/net/websocket"
)

const (
	tencentASRDefaultEndpoint        = "wss://asr.cloud.tencent.com/asr/v2"
	tencentASRDefaultEngineModelType = "16k_en"
	tencentASRDefaultVoiceFormat     = "1"
	tencentASRDefaultNeedVAD         = "1"
	tencentASRDefaultConvertNumMode  = "1"
	tencentASRDefaultFilterEmpty     = "1"
	tencentASRDefaultTimeout         = 30 * time.Second
	tencentASRDefaultCloseTimeout    = 3 * time.Second
	tencentASRStreamLogEveryFrames   = 50
	tencentASRStreamLogEveryDuration = 5 * time.Second
)

type TencentProvider struct {
	config Config
	dial   func(context.Context, string) (tencentASRConn, error)
	now    func() time.Time
	voice  func() string
}

type tencentASRConn interface {
	ReadJSON(v any) error
	WriteBinary(payload []byte) error
	WriteText(payload []byte) error
	Close() error
}

type tencentASRConfig struct {
	AppID           string
	SecretID        string
	SecretKey       string
	Endpoint        string
	EngineModelType string
	VoiceFormat     string
	InputSampleRate string
	NeedVAD         string
	Params          map[string]string
	Timeout         time.Duration
}

type tencentASRResponse struct {
	Code      int                    `json:"code"`
	Message   string                 `json:"message"`
	VoiceID   string                 `json:"voice_id,omitempty"`
	MessageID string                 `json:"message_id,omitempty"`
	Final     uint32                 `json:"final,omitempty"`
	Result    tencentASRResultObject `json:"result,omitempty"`
}

type tencentASRResultObject struct {
	SliceType    uint32 `json:"slice_type"`
	Index        int    `json:"index"`
	StartTime    uint32 `json:"start_time"`
	EndTime      uint32 `json:"end_time"`
	VoiceTextStr string `json:"voice_text_str"`
}

type tencentXNetConn struct {
	conn *xwebsocket.Conn
}

type tencentASRStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	conn       tencentASRConn
	provider   string
	callID     string
	voiceID    string
	startedAt  time.Time
	results    chan Result
	errors     chan error
	done       chan struct{}
	writeMu    sync.Mutex
	closeOnce  sync.Once
	frameCount int
	audioBytes int
	lastLogged time.Time
}

// NewTencentProvider 创建腾讯云实时 ASR provider。
func NewTencentProvider(config Config) *TencentProvider {
	return &TencentProvider{
		config: config,
		dial:   dialTencentASR,
		now:    time.Now,
		voice:  defaultTencentASRVoiceID,
	}
}

// Name 返回 provider 名称。
func (p *TencentProvider) Name() string {
	if p.config.Provider == "" {
		return "tencent-asr"
	}
	return p.config.Provider
}

// Recognize 调用腾讯云实时 ASR WebSocket 识别一批音频帧。
// 逻辑:
// 1. 打开同一套腾讯长连接实时流，避免维护另一套批量 WebSocket 写法。
// 2. 把传入帧连续写入流，最后发送 end 并等待腾讯返回 final。
// 3. 汇总流式 partial/final 结果后返回，供旧接口和测试继续使用。
func (p *TencentProvider) Recognize(ctx context.Context, req Request) ([]Result, error) {
	if len(req.Frames) == 0 {
		return nil, nil
	}
	config := buildTencentASRConfig(req.Config)
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}
	callID, audioBytes := summarizeFrames(req.Frames)
	slog.InfoContext(ctx, "腾讯 ASR 批量识别转入流式通道",
		slog.String("provider", p.Name()),
		slog.String("callId", callID),
		slog.Int("frameCount", len(req.Frames)),
		slog.Int("audioBytes", audioBytes),
	)
	stream, err := p.OpenStream(ctx, StreamRequest{Config: req.Config, CallID: callID})
	if err != nil {
		return nil, err
	}
	for _, frame := range req.Frames {
		if err := stream.Write(ctx, frame); err != nil {
			_ = stream.Close(ctx)
			return nil, err
		}
	}
	closeErr := stream.Close(ctx)
	results, err := collectASRStreamOutputs(stream)
	if err != nil {
		return results, err
	}
	if closeErr != nil {
		return results, closeErr
	}
	slog.InfoContext(ctx, "腾讯 ASR 调用完成",
		slog.String("provider", p.Name()),
		slog.String("callId", callID),
		slog.Int("resultCount", len(results)),
	)
	return results, nil
}

// OpenStream 建立腾讯 ASR 长连接实时流。
// 逻辑:
// 1. 从通用配置提取腾讯参数并创建一次签名 URL。
// 2. 建立 WebSocket 并读取启动确认，随后返回可持续写入音频的 Stream。
// 3. 后台读取腾讯 partial/final 结果并推入 Results channel，调用方只负责连续 Write 和结束 Close。
func (p *TencentProvider) OpenStream(ctx context.Context, req StreamRequest) (Stream, error) {
	config := buildTencentASRConfig(req.Config)
	if config.AppID == "" || config.SecretID == "" || config.SecretKey == "" {
		return nil, errors.New("tencent asr missing appId/secretId/secretKey")
	}
	voiceID := p.voice()
	rawURL, summaryURL, err := buildTencentASRSignedURL(config, p.now(), voiceID)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "腾讯 ASR 开始流式连接",
		slog.String("provider", p.Name()),
		slog.String("callId", req.CallID),
		slog.String("voiceId", voiceID),
		slog.String("endpoint", config.Endpoint),
		slog.String("engineModelType", config.EngineModelType),
		slog.String("voiceFormat", config.VoiceFormat),
		slog.String("urlPreview", summaryURL),
	)
	conn, err := p.dial(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if err := readTencentASRStart(ctx, conn, voiceID); err != nil {
		_ = conn.Close()
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &tencentASRStream{
		ctx:        streamCtx,
		cancel:     cancel,
		conn:       conn,
		provider:   p.Name(),
		callID:     req.CallID,
		voiceID:    voiceID,
		startedAt:  time.Now(),
		results:    make(chan Result, 16),
		errors:     make(chan error, 4),
		done:       make(chan struct{}),
		lastLogged: time.Now(),
	}
	go stream.readLoop()
	return stream, nil
}

// Write 向腾讯 ASR 长连接写入一帧音频。
// 逻辑:
// 1. 空 payload 直接忽略，避免把控制意义不明的二进制帧发给腾讯。
// 2. 同一连接的写操作串行化，防止音频帧和 end 控制帧交叉。
// 3. 按帧数或时间间隔打印累计写入量，确认后端正在持续喂音频。
func (s *tencentASRStream) Write(ctx context.Context, frame Frame) error {
	if len(frame.Payload) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.writeMu.Lock()
	err := s.conn.WriteBinary(frame.Payload)
	if err == nil {
		s.frameCount++
		s.audioBytes += len(frame.Payload)
		s.logWriteProgressLocked(ctx, len(frame.Payload))
	}
	s.writeMu.Unlock()
	return err
}

// Close 结束腾讯 ASR 长连接输入并等待服务端 final。
// 逻辑:
// 1. 串行发送 {"type":"end"}，告诉腾讯当前音频流结束。
// 2. 等待读循环收到 final=1；超时或调用方取消时主动关闭连接释放阻塞读。
// 3. Close 可重复调用，只有第一次会真正发送 end。
func (s *tencentASRStream) Close(ctx context.Context) error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.writeMu.Lock()
		closeErr = s.conn.WriteText([]byte(`{"type":"end"}`))
		s.writeMu.Unlock()
		if closeErr != nil {
			s.cancel()
			_ = s.conn.Close()
			return
		}
		timer := time.NewTimer(tencentASRDefaultCloseTimeout)
		defer timer.Stop()
		select {
		case <-s.done:
		case <-ctx.Done():
			closeErr = ctx.Err()
			s.cancel()
			_ = s.conn.Close()
		case <-timer.C:
			closeErr = context.DeadlineExceeded
			slog.WarnContext(ctx, "腾讯 ASR 流式关闭等待 final 超时，已主动关闭连接",
				slog.String("provider", s.provider),
				slog.String("callId", s.callID),
				slog.String("voiceId", s.voiceID),
				slog.Int("frameCount", s.frameCount),
				slog.Int("audioBytes", s.audioBytes),
				slog.Duration("timeout", tencentASRDefaultCloseTimeout),
			)
			s.cancel()
			_ = s.conn.Close()
		}
	})
	return closeErr
}

// Results 返回腾讯 ASR 流式识别结果 channel。
func (s *tencentASRStream) Results() <-chan Result {
	return s.results
}

// Errors 返回腾讯 ASR 流式错误 channel。
func (s *tencentASRStream) Errors() <-chan error {
	return s.errors
}

// readLoop 持续读取腾讯 ASR 事件并输出 partial/final 结果。
// 逻辑:
// 1. 每条 JSON 先检查 code，非 0 立即作为 provider 错误上报。
// 2. 有文本时按 slice_type 转成 partial/final Result 推给调用方。
// 3. 收到 final=1 表示整条音频流结束，关闭结果和错误 channel。
func (s *tencentASRStream) readLoop() {
	defer close(s.done)
	defer close(s.results)
	defer close(s.errors)
	defer s.conn.Close()
	for {
		if err := s.ctx.Err(); err != nil {
			return
		}
		var msg tencentASRResponse
		if err := s.conn.ReadJSON(&msg); err != nil {
			if s.ctx.Err() == nil {
				s.emitError(err)
			}
			return
		}
		if msg.Code != 0 {
			s.emitError(fmt.Errorf("tencent asr failed code=%d message=%s", msg.Code, msg.Message))
			return
		}
		if result, ok := tencentASRResultFromResponse(s.callID, msg); ok {
			slog.InfoContext(s.ctx, "腾讯 ASR 收到识别结果",
				slog.String("provider", s.provider),
				slog.String("callId", s.callID),
				slog.String("voiceId", s.voiceID),
				slog.Bool("isFinal", result.IsFinal),
				slog.Int("sliceType", int(msg.Result.SliceType)),
				slog.Int("index", msg.Result.Index),
				slog.Int("textRunes", len([]rune(result.Text))),
				slog.String("textPreview", previewASRProviderText(result.Text, 80)),
			)
			select {
			case s.results <- result:
			case <-s.ctx.Done():
				return
			}
		}
		if msg.Final == 1 {
			slog.InfoContext(s.ctx, "腾讯 ASR 流式识别完成",
				slog.String("provider", s.provider),
				slog.String("callId", s.callID),
				slog.String("voiceId", s.voiceID),
				slog.Int("frameCount", s.frameCount),
				slog.Int("audioBytes", s.audioBytes),
				slog.Duration("duration", time.Since(s.startedAt)),
			)
			return
		}
	}
}

// emitError 非阻塞写入腾讯 ASR 流式错误，避免读循环被无人消费的错误 channel 卡住。
func (s *tencentASRStream) emitError(err error) {
	if err == nil {
		return
	}
	select {
	case s.errors <- err:
	default:
	}
}

// logWriteProgressLocked 在持有写锁时打印腾讯 ASR 音频写入进度。
func (s *tencentASRStream) logWriteProgressLocked(ctx context.Context, frameBytes int) {
	if s.frameCount != 1 &&
		s.frameCount%tencentASRStreamLogEveryFrames != 0 &&
		time.Since(s.lastLogged) < tencentASRStreamLogEveryDuration {
		return
	}
	s.lastLogged = time.Now()
	msg := "腾讯 ASR 已写入首帧音频"
	if s.frameCount > 1 {
		msg = "腾讯 ASR 持续写入音频"
	}
	slog.InfoContext(ctx, msg,
		slog.String("provider", s.provider),
		slog.String("callId", s.callID),
		slog.String("voiceId", s.voiceID),
		slog.Int("frameCount", s.frameCount),
		slog.Int("audioBytes", s.audioBytes),
		slog.Int("frameBytes", frameBytes),
		slog.Duration("duration", time.Since(s.startedAt)),
	)
}

// tencentASRResultFromResponse 把腾讯 ASR JSON 事件转换为通用 Result。
func tencentASRResultFromResponse(callID string, msg tencentASRResponse) (Result, bool) {
	text := strings.TrimSpace(msg.Result.VoiceTextStr)
	if text == "" {
		return Result{}, false
	}
	return Result{
		CallID:     callID,
		Text:       text,
		IsFinal:    msg.Result.SliceType == 2,
		Confidence: 0.95,
	}, true
}

func previewASRProviderText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// collectASRStreamOutputs 在流关闭后汇总结果和错误，服务旧批量接口。
func collectASRStreamOutputs(stream Stream) ([]Result, error) {
	var results []Result
	var firstErr error
	resultCh := stream.Results()
	errorCh := stream.Errors()
	for resultCh != nil || errorCh != nil {
		select {
		case result, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			results = append(results, result)
		case err, ok := <-errorCh:
			if !ok {
				errorCh = nil
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return results, firstErr
}

// buildTencentASRConfig 从通用 ASR Config 中提取腾讯云实时识别参数。
func buildTencentASRConfig(config Config) tencentASRConfig {
	endpoint := firstNonEmpty(config.Endpoint, config.Params["endpoint"], tencentASRDefaultEndpoint)
	model := firstNonEmpty(config.Params["engine_model_type"], config.Model, tencentASRDefaultEngineModelType)
	voiceFormat := normalizeTencentASRVoiceFormat(firstNonEmpty(config.Params["voice_format"], tencentASRDefaultVoiceFormat))
	params := cloneParams(config.Params)
	params["engine_model_type"] = model
	params["voice_format"] = voiceFormat
	inputSampleRate := normalizeTencentASRInputSampleRate(voiceFormat, params["input_sample_rate"])
	if inputSampleRate == "" {
		delete(params, "input_sample_rate")
	} else {
		params["input_sample_rate"] = inputSampleRate
	}
	params["needvad"] = firstNonEmpty(params["needvad"], tencentASRDefaultNeedVAD)
	params["convert_num_mode"] = firstNonEmpty(params["convert_num_mode"], tencentASRDefaultConvertNumMode)
	params["filter_empty_result"] = firstNonEmpty(params["filter_empty_result"], tencentASRDefaultFilterEmpty)
	return tencentASRConfig{
		AppID:           firstNonEmpty(config.Params["appId"], config.Params["appid"]),
		SecretID:        firstNonEmpty(config.Secrets["secretId"], config.Secrets["secretID"]),
		SecretKey:       firstNonEmpty(config.Secrets["secretKey"], config.Secrets["secret"]),
		Endpoint:        endpoint,
		EngineModelType: model,
		VoiceFormat:     voiceFormat,
		InputSampleRate: inputSampleRate,
		NeedVAD:         params["needvad"],
		Params:          params,
		Timeout:         tencentASRDefaultTimeout,
	}
}

// buildTencentASRSignedURL 生成腾讯 ASR WebSocket URL 和脱敏后的日志预览。
func buildTencentASRSignedURL(config tencentASRConfig, now time.Time, voiceID string) (string, string, error) {
	endpoint, err := parseTencentASREndpoint(config.Endpoint)
	if err != nil {
		return "", "", err
	}
	timestamp := now.Unix()
	query := map[string]string{
		"secretid":          config.SecretID,
		"timestamp":         strconv.FormatInt(timestamp, 10),
		"expired":           strconv.FormatInt(timestamp+24*60*60, 10),
		"nonce":             strconv.FormatInt(timestamp, 10),
		"engine_model_type": config.EngineModelType,
		"voice_id":          voiceID,
		"voice_format":      config.VoiceFormat,
		"needvad":           config.NeedVAD,
	}
	for key, value := range config.Params {
		if key == "" || value == "" || key == "appId" || key == "appid" || key == "endpoint" {
			continue
		}
		query[key] = value
	}
	if config.InputSampleRate != "" {
		query["input_sample_rate"] = config.InputSampleRate
	}
	queryString := encodeTencentASRQuery(query)
	serverURL := endpoint.Host + endpoint.Path + "/" + config.AppID + "?" + queryString
	signature := hmacSHA1Base64(config.SecretKey, serverURL)
	rawURL := endpoint.Scheme + "://" + serverURL + "&signature=" + url.QueryEscape(signature)
	summaryQuery := encodeTencentASRQuery(redactTencentASRQuery(query))
	summary := endpoint.Scheme + "://" + endpoint.Host + endpoint.Path + "/" + config.AppID + "?" + summaryQuery + "&signature=[REDACTED]"
	return rawURL, summary, nil
}

// redactTencentASRQuery 复制腾讯 ASR query 并脱敏日志预览中的敏感字段。
func redactTencentASRQuery(query map[string]string) map[string]string {
	out := make(map[string]string, len(query))
	for key, value := range query {
		if strings.EqualFold(key, "secretid") {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = value
	}
	return out
}

// parseTencentASREndpoint 解析 ASR endpoint，并归一化到 /asr/v2 路径。
func parseTencentASREndpoint(raw string) (*url.URL, error) {
	if raw == "" {
		raw = tencentASRDefaultEndpoint
	}
	if !strings.Contains(raw, "://") {
		raw = "wss://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "wss"
	}
	if parsed.Host == "" {
		return nil, errors.New("tencent asr endpoint missing host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/asr/v2"
	}
	return parsed, nil
}

// encodeTencentASRQuery 按腾讯官方 SDK 方式对 query key 排序并拼接签名原文。
func encodeTencentASRQuery(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "&")
}

// readTencentASRStart 读取并校验腾讯 ASR 连接启动确认。
func readTencentASRStart(ctx context.Context, conn tencentASRConn, voiceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var msg tencentASRResponse
	if err := conn.ReadJSON(&msg); err != nil {
		return err
	}
	if msg.Code != 0 {
		return fmt.Errorf("tencent asr start failed voice_id=%s code=%d message=%s", voiceID, msg.Code, msg.Message)
	}
	return nil
}

// dialTencentASR 建立腾讯 ASR WebSocket 连接。
func dialTencentASR(_ context.Context, rawURL string) (tencentASRConn, error) {
	origin := "https://asr.cloud.tencent.com/"
	config, err := xwebsocket.NewConfig(rawURL, origin)
	if err != nil {
		return nil, err
	}
	config.TlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	conn, err := xwebsocket.DialConfig(config)
	if err != nil {
		return nil, err
	}
	return &tencentXNetConn{conn: conn}, nil
}

// ReadJSON 读取一个腾讯 ASR JSON 文本事件。
func (c *tencentXNetConn) ReadJSON(v any) error {
	return xwebsocket.JSON.Receive(c.conn, v)
}

// WriteBinary 发送一帧音频二进制数据。
func (c *tencentXNetConn) WriteBinary(payload []byte) error {
	c.conn.PayloadType = xwebsocket.BinaryFrame
	_, err := c.conn.Write(payload)
	return err
}

// WriteText 发送一个文本控制消息。
func (c *tencentXNetConn) WriteText(payload []byte) error {
	c.conn.PayloadType = xwebsocket.TextFrame
	_, err := c.conn.Write(payload)
	return err
}

// Close 关闭 WebSocket 连接。
func (c *tencentXNetConn) Close() error {
	return c.conn.Close()
}

// hmacSHA1Base64 按腾讯实时 ASR 要求生成 HMAC-SHA1 base64 签名。
func hmacSHA1Base64(secret, value string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// normalizeTencentASRVoiceFormat 归一化 voice_format，便于前端填 opus/pcm 等可读值。
func normalizeTencentASRVoiceFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "pcm", "1", "":
		return "1"
	case "opus", "10":
		return "10"
	case "wav", "12":
		return "12"
	case "mp3", "8":
		return "8"
	default:
		return strings.TrimSpace(format)
	}
}

// normalizeTencentASRInputSampleRate 归一化 input_sample_rate：腾讯只允许 PCM 8k 上采样场景传 8000。
func normalizeTencentASRInputSampleRate(voiceFormat, sampleRate string) string {
	if normalizeTencentASRVoiceFormat(voiceFormat) != "1" {
		return ""
	}
	if strings.TrimSpace(sampleRate) == "8000" {
		return "8000"
	}
	return ""
}

// defaultTencentASRVoiceID 生成腾讯 ASR voice_id。
func defaultTencentASRVoiceID() string {
	return fmt.Sprintf("pbx-%d", time.Now().UnixNano())
}

// cloneParams 复制参数 map，避免修改调用方传入对象。
func cloneParams(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		if value != "" {
			out[key] = value
		}
	}
	return out
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

// marshalTencentASRResponse 生成测试和日志中使用的 JSON 文本。
func marshalTencentASRResponse(response tencentASRResponse) []byte {
	payload, _ := json.Marshal(response)
	return payload
}

