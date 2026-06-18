// PBX WebSocket控制通道：事件适配+服务端
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/tts"
	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxevents "github.com/SATA260/SimulSpeak1/internal/pbx/events"
	"github.com/SATA260/SimulSpeak1/internal/pbx/webrtc"
	pbxprotocol "github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
)

type Message = pbxprotocol.Message

const (
	TypeWebRTCOffer       = pbxprotocol.TypeWebRTCOffer
	TypeWebRTCAnswer      = pbxprotocol.TypeWebRTCAnswer
	TypeICE               = pbxprotocol.TypeICE
	TypeASRResult         = pbxprotocol.TypeASRResult
	TypeTranslationResult = pbxprotocol.TypeTranslationResult
	TypeTTSCommand        = pbxprotocol.TypeTTSCommand
	TypeTTSResult         = pbxprotocol.TypeTTSResult
	TypeRecordingResult   = pbxprotocol.TypeRecordingResult
	TypeCloseSession      = pbxprotocol.TypeCloseSession
	TypeError             = pbxprotocol.TypeError
	TypePing              = pbxprotocol.TypePing
	TypePong              = pbxprotocol.TypePong
)

type Server struct {
	WebRTC          *webrtc.Manager
	ProviderConfigs map[model.CapabilityType]model.ProviderConfig
	EventBus        *eventbus.MemoryBus
	initMu          sync.Mutex
	languageMu      sync.RWMutex
	sessionLanguage map[string]model.SessionLanguageOptions
}

type serverConn struct {
	netConn net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	mu      sync.Mutex
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !pbxprotocol.IsWebSocketRequest(r) {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return
	}
	if s.WebRTC == nil {
		http.Error(w, "webrtc manager is not configured", http.StatusServiceUnavailable)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return
	}
	netConn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", pbxprotocol.WebSocketAccept(key))
	if err != nil {
		_ = netConn.Close()
		return
	}
	if err := rw.Flush(); err != nil {
		_ = netConn.Close()
		return
	}
	conn := &serverConn{netConn: netConn, reader: rw.Reader, writer: rw.Writer}
	s.serveConn(r.Context(), conn)
}

func (s *Server) serveConn(ctx context.Context, conn *serverConn) {
	bus := s.ensureEventBus()
	adapter := newEventAdapter(bus, conn)
	adapter.Start(ctx)
	defer func() {
		_ = conn.netConn.Close()
		adapter.Stop()
	}()
	for {
		opcode, payload, err := pbxprotocol.ReadFrame(conn.reader, true)
		if err != nil {
			return
		}
		switch opcode {
		case 0x1:
			var message Message
			if err := json.Unmarshal(payload, &message); err != nil {
				_ = conn.writeJSON(Message{Type: TypeError, Error: "invalid json"})
				continue
			}
			s.handleMessage(ctx, conn, adapter, message)
		case 0x8:
			_ = conn.writeClose()
			return
		case 0x9:
			_ = conn.writeFrame(0xA, payload)
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, conn *serverConn, adapter *eventAdapter, message Message) {
	switch message.Type {
	case TypeWebRTCOffer:
		s.handleOffer(ctx, adapter, message)
	case TypeICE:
		if err := s.WebRTC.AddICECandidate(ctx, message.ConnectionID, message.Candidate); err != nil {
			s.publishError(ctx, message, err, map[string]string{"source": "remote_ice"})
		}
	case TypeTTSCommand:
		go s.handleTTS(ctx, message)
	case TypeCloseSession:
		s.WebRTC.CloseConnection(message.ConnectionID)
		s.clearSessionLanguage(message.ConnectionID)
	case TypePing:
		_ = conn.writeJSON(Message{Type: TypePong, RequestID: message.RequestID, ConnectionID: message.ConnectionID})
	default:
		_ = conn.writeJSON(errorMessage(message, fmt.Errorf("unknown pbx control message type %q", message.Type)))
	}
}

func (s *Server) handleOffer(ctx context.Context, adapter *eventAdapter, message Message) {
	adapter.BindConnection(message.ConnectionID)
	language, err := model.NormalizeSessionLanguageOptions(message.Metadata)
	if err != nil {
		s.publishError(ctx, message, err, map[string]string{"source": "language"})
		return
	}
	s.setSessionLanguage(message.ConnectionID, language)
	message.Metadata = mergeControlMetadata(message.Metadata, language.Metadata())
	providerConfigs := model.CloneProviderConfigs(s.ProviderConfigs)
	answer, err := s.WebRTC.AcceptOffer(ctx, webrtc.OfferRequest{
		ConnectionID:    message.ConnectionID,
		TenantID:        message.TenantID,
		CallID:          message.CallID,
		UserID:          message.UserID,
		SDP:             message.SDP,
		Metadata:        message.Metadata,
		ProviderConfigs: providerConfigs,
	})
	if err != nil {
		s.publishError(ctx, message, err, map[string]string{"source": "webrtc_offer"})
		return
	}
	s.publishRequired(ctx, pbxevents.NewWebRTCAnswerEvent(pbxevents.WebRTCAnswerPayload{
		RequestID:    message.RequestID,
		ConnectionID: message.ConnectionID,
		CallID:       message.CallID,
		UserID:       message.UserID,
		SDP:          answer,
	}))
}

func (s *Server) handleTTS(ctx context.Context, message Message) {
	result, err := s.synthesizeTTS(ctx, message)
	if err != nil {
		if message.Sequence > 0 {
			s.WebRTC.SkipAudioSequence(ctx, message.ConnectionID, message.Sequence, err.Error())
		}
		s.publishError(ctx, message, err, map[string]string{"source": "tts"})
		return
	}
	s.publishRequired(ctx, pbxevents.NewTTSResultEvent(pbxevents.TTSResultPayload{
		RequestID:    result.RequestID,
		ConnectionID: result.ConnectionID,
		CallID:       result.CallID,
		UserID:       result.UserID,
		UtteranceID:  result.UtteranceID,
		Text:         result.Text,
		Format:       result.Format,
		SampleRate:   result.SampleRate,
		IsLast:       result.IsLast,
		Voice:        result.Voice,
		Language:     result.Language,
		Sequence:     result.Sequence,
		Metadata:     result.Metadata,
	}))
}

func (s *Server) synthesizeTTS(ctx context.Context, message Message) (Message, error) {
	if strings.TrimSpace(message.Text) == "" {
		return Message{}, errors.New("tts text is empty")
	}
	message = s.applyTTSLanguageDefaults(message)
	config := s.ProviderConfigs[model.CapabilityTypeTTS]
	client := tts.NewClient(ttsConfigFromProviderConfig(config, message))
	chunks, err := client.Synthesize(ctx, message.Text, tts.Options{
		CallID:      message.CallID,
		UtteranceID: message.UtteranceID,
		Voice:       message.Voice,
		Language:    message.Language,
	})
	if err != nil {
		return Message{}, err
	}
	playbackChunks := make([]webrtc.PlaybackChunk, 0, len(chunks))
	var originalBytes int
	for _, chunk := range chunks {
		originalBytes += len(chunk.Audio)
		playbackChunks = append(playbackChunks, webrtc.PlaybackChunk{
			CallID:     chunk.CallID,
			Payload:    chunk.Audio,
			Format:     chunk.Format,
			SampleRate: chunk.SampleRate,
			Sequence:   message.Sequence,
		})
	}
	playback, err := s.WebRTC.PlayAudio(ctx, message.ConnectionID, playbackChunks)
	if err != nil {
		return Message{}, err
	}
	slog.InfoContext(ctx, "PBX control TTS 音频已进入 WebRTC 下行通道",
		slog.String("connectionId", message.ConnectionID),
		slog.String("callId", message.CallID),
		slog.Int("chunks", playback.Chunks),
		slog.Int("originalAudioBytes", originalBytes),
		slog.Int("pcmuFrames", playback.FrameCount),
		slog.Int("sequence", message.Sequence),
		slog.Bool("queued", playback.Queued),
	)
	return Message{
		Type:         TypeTTSResult,
		RequestID:    message.RequestID,
		ConnectionID: message.ConnectionID,
		CallID:       message.CallID,
		UserID:       message.UserID,
		UtteranceID:  message.UtteranceID,
		Text:         message.Text,
		Format:       "pcmu",
		SampleRate:   8000,
		IsLast:       true,
		Voice:        message.Voice,
		Language:     message.Language,
		Sequence:     message.Sequence,
		Metadata: map[string]string{
			"audioTransport":     "webrtc",
			"provider":           config.Provider,
			"queued":             strconv.FormatBool(playback.Queued),
			"chunkCount":         strconv.Itoa(playback.Chunks),
			"pcmuFrameCount":     strconv.Itoa(playback.FrameCount),
			"originalAudioBytes": strconv.Itoa(originalBytes),
			"sequence":           strconv.Itoa(message.Sequence),
		},
	}, nil
}

func (s *Server) setSessionLanguage(connectionID string, language model.SessionLanguageOptions) {
	if strings.TrimSpace(connectionID) == "" {
		return
	}
	s.languageMu.Lock()
	defer s.languageMu.Unlock()
	if s.sessionLanguage == nil {
		s.sessionLanguage = map[string]model.SessionLanguageOptions{}
	}
	s.sessionLanguage[connectionID] = language.WithDefaults()
}

func (s *Server) clearSessionLanguage(connectionID string) {
	s.languageMu.Lock()
	defer s.languageMu.Unlock()
	delete(s.sessionLanguage, connectionID)
}

func (s *Server) languageForSession(connectionID string) model.SessionLanguageOptions {
	s.languageMu.RLock()
	language := s.sessionLanguage[connectionID]
	s.languageMu.RUnlock()
	if language.SourceLanguage == "" {
		return model.DefaultSessionLanguageOptions()
	}
	return language.WithDefaults()
}

func (s *Server) applyTTSLanguageDefaults(message Message) Message {
	language := s.languageForSession(message.ConnectionID)
	if strings.TrimSpace(message.Language) == "" {
		message.Language = language.TTSLanguage
	}
	if strings.TrimSpace(message.Voice) == "" {
		message.Voice = language.TTSVoiceType
	}
	message.Metadata = mergeControlMetadata(message.Metadata, map[string]string{
		model.LanguageMetadataSourceLanguage:     language.SourceLanguage,
		model.LanguageMetadataTargetLanguage:     language.TargetLanguage,
		model.LanguageMetadataTTSPrimaryLanguage: strconv.Itoa(language.TTSPrimaryLanguage),
	})
	return message
}

func (s *Server) ensureEventBus() *eventbus.MemoryBus {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.EventBus == nil {
		s.EventBus = eventbus.NewMemoryBus()
	}
	if s.WebRTC != nil {
		s.WebRTC.SetEventBus(s.EventBus)
	}
	return s.EventBus
}

func (s *Server) publishRequired(ctx context.Context, event model.DomainEvent) {
	bus := s.ensureEventBus()
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := bus.PublishRequired(publishCtx, pbxevents.Topic, event); err != nil {
		slog.WarnContext(ctx, "PBX control required event 发布失败",
			slog.String("eventType", string(event.Type)),
			slog.Any("error", err),
		)
	}
}

func (s *Server) publishError(ctx context.Context, message Message, err error, metadata map[string]string) {
	if err == nil {
		return
	}
	s.publishRequired(ctx, pbxevents.NewErrorEvent(pbxevents.ErrorPayload{
		RequestID:    message.RequestID,
		ConnectionID: message.ConnectionID,
		CallID:       message.CallID,
		UserID:       message.UserID,
		Error:        err.Error(),
		Metadata:     compactMetadata(metadata),
	}))
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

func mergeControlMetadata(base map[string]string, overrides map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range base {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	for key, value := range overrides {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *serverConn) writeJSON(message Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, data)
}

func (c *serverConn) writeClose() error {
	return c.writeFrame(0x8, nil)
}

func (c *serverConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.netConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return pbxprotocol.WriteFrame(c.writer, opcode, payload, false)
}

func errorMessage(message Message, err error) Message {
	return Message{
		Type:         TypeError,
		RequestID:    message.RequestID,
		ConnectionID: message.ConnectionID,
		CallID:       message.CallID,
		UserID:       message.UserID,
		Error:        err.Error(),
	}
}

func ttsConfigFromProviderConfig(provider model.ProviderConfig, message Message) tts.Config {
	params := cloneStringMap(provider.Params)
	secrets := cloneStringMap(provider.Secrets)
	if params == nil {
		params = map[string]string{}
	}
	if primaryLanguage := strings.TrimSpace(message.Metadata[model.LanguageMetadataTTSPrimaryLanguage]); primaryLanguage != "" {
		params["primaryLanguage"] = primaryLanguage
	}
	name := firstNonEmpty(provider.Provider, "mock")
	format := firstNonEmpty(params["codec"], "pcm")
	voice := firstNonEmpty(message.Voice, params["voiceType"])
	language := firstNonEmpty(message.Language, params["language"], "en-US")
	if name == "tencent-tts" {
		format = firstNonEmpty(params["codec"], "wav")
		voice = firstNonEmpty(message.Voice, params["voiceType"], "101001")
		language = firstNonEmpty(message.Language, params["language"], "zh-CN")
	}
	rate := firstNonZero(parseInt(params["sampleRate"]), 16000)
	return tts.Config{
		Provider:        name,
		APIKey:          firstNonEmpty(secrets["apiKey"], secrets["api_key"], secrets["token"]),
		Endpoint:        provider.Endpoint,
		Params:          params,
		Secrets:         secrets,
		DefaultVoice:    voice,
		DefaultLanguage: language,
		DefaultFormat:   format,
		DefaultRate:     rate,
		Voices:          []string{voice},
		Languages:       []string{language},
		SampleRates:     []int{rate},
	}
}

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
