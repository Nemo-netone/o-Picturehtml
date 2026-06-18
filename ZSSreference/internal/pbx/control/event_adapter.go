// PBX WebSocket控制通道：事件适配+服务端
package control

import (
	"context"
	"log/slog"
	"strconv"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxevents "github.com/SATA260/SimulSpeak1/internal/pbx/events"
)

type eventAdapter struct {
	bus         *eventbus.MemoryBus
	conn        *serverConn
	events      <-chan model.DomainEvent
	unsubscribe func()
	stop        chan struct{}
	done        chan struct{}

	mu         sync.Mutex
	active     map[string]struct{}
	answerSent map[string]bool
	pendingICE map[string][]Message
}

func newEventAdapter(bus *eventbus.MemoryBus, conn *serverConn) *eventAdapter {
	return &eventAdapter{
		bus:        bus,
		conn:       conn,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		active:     map[string]struct{}{},
		answerSent: map[string]bool{},
		pendingICE: map[string][]Message{},
	}
}

func (a *eventAdapter) Start(ctx context.Context) {
	a.events, a.unsubscribe = a.bus.SubscribeWithOptions(pbxevents.Topic, eventbus.SubscribeOptions{Buffer: 256})
	go a.loop(ctx)
}

func (a *eventAdapter) Stop() {
	if a.unsubscribe != nil {
		a.unsubscribe()
	}
	close(a.stop)
	<-a.done
}

func (a *eventAdapter) BindConnection(connectionID string) {
	if connectionID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active[connectionID] = struct{}{}
	if _, ok := a.answerSent[connectionID]; !ok {
		a.answerSent[connectionID] = false
	}
}

func (a *eventAdapter) loop(ctx context.Context) {
	defer close(a.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stop:
			return
		case event := <-a.events:
			a.handleEvent(ctx, event)
		}
	}
}

func (a *eventAdapter) handleEvent(ctx context.Context, event model.DomainEvent) {
	switch event.Type {
	case model.EventTypePBXWebRTCAnswerCreated:
		a.handleAnswerEvent(ctx, event)
	case model.EventTypePBXICECandidate:
		a.handleICEEvent(ctx, event)
	case model.EventTypePBXASRResult:
		a.handleASREvent(ctx, event)
	case model.EventTypePBXTranslationResult:
		a.handleTranslationEvent(ctx, event)
	case model.EventTypePBXTTSResult:
		a.handleTTSEvent(ctx, event)
	case model.EventTypePBXRecordingResult:
		a.handleRecordingEvent(ctx, event)
	case model.EventTypePBXError:
		a.handleErrorEvent(ctx, event)
	}
}

func (a *eventAdapter) handleAnswerEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.WebRTCAnswerPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	message := Message{
		Type:         TypeWebRTCAnswer,
		RequestID:    payload.RequestID,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		SDP:          payload.SDP,
		Metadata:     payload.Metadata,
	}
	if err := a.conn.writeJSON(message); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入 WebRTC answer 失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.Any("error", err),
		)
		return
	}
	a.mu.Lock()
	a.answerSent[payload.ConnectionID] = true
	pendingICE := append([]Message(nil), a.pendingICE[payload.ConnectionID]...)
	delete(a.pendingICE, payload.ConnectionID)
	a.mu.Unlock()
	for _, ice := range pendingICE {
		if err := a.conn.writeJSON(ice); err != nil {
			slog.WarnContext(ctx, "PBX event adapter flush ICE 失败",
				slog.String("connectionId", payload.ConnectionID),
				slog.Any("error", err),
			)
			return
		}
	}
}

func (a *eventAdapter) handleICEEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.ICECandidatePayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	message := Message{
		Type:         TypeICE,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		Candidate:    payload.Candidate,
		Metadata:     payload.Metadata,
	}
	a.mu.Lock()
	answerSent := a.answerSent[payload.ConnectionID]
	if !answerSent {
		a.pendingICE[payload.ConnectionID] = append(a.pendingICE[payload.ConnectionID], message)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	if err := a.conn.writeJSON(message); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入 ICE 失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) handleASREvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.ASRResultPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	if err := a.conn.writeJSON(Message{
		Type:         TypeASRResult,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		UtteranceID:  payload.UtteranceID,
		Text:         payload.Text,
		IsFinal:      payload.IsFinal,
		Confidence:   payload.Confidence,
		Language:     payload.Language,
		Metadata:     payload.Metadata,
	}); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入 ASR 结果失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.String("utteranceId", payload.UtteranceID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) handleTranslationEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.TranslationResultPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	if err := a.conn.writeJSON(Message{
		Type:         TypeTranslationResult,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		UtteranceID:  payload.UtteranceID,
		SourceText:   payload.SourceText,
		Text:         payload.Text,
		IsFinal:      payload.IsFinal,
		Engine:       payload.Engine,
		Revised:      payload.Revised,
		Language:     payload.Language,
		Metadata:     payload.Metadata,
	}); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入翻译结果失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.String("utteranceId", payload.UtteranceID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) handleTTSEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.TTSResultPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	if err := a.conn.writeJSON(Message{
		Type:         TypeTTSResult,
		RequestID:    payload.RequestID,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		UtteranceID:  payload.UtteranceID,
		Text:         payload.Text,
		Format:       payload.Format,
		SampleRate:   payload.SampleRate,
		IsLast:       payload.IsLast,
		Voice:        payload.Voice,
		Language:     payload.Language,
		Sequence:     payload.Sequence,
		Metadata:     payload.Metadata,
	}); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入 TTS 结果失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.String("utteranceId", payload.UtteranceID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) handleRecordingEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.RecordingResultPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	metadata := payload.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["recordingId"] = payload.RecordingID
	metadata["objectKey"] = payload.ObjectKey
	metadata["checksum"] = payload.Checksum
	metadata["size"] = strconv.FormatInt(payload.Size, 10)
	metadata["sampleRate"] = strconv.Itoa(payload.SampleRate)
	metadata["format"] = payload.Format
	metadata["startedAt"] = payload.StartedAt
	metadata["stoppedAt"] = payload.StoppedAt
	if err := a.conn.writeJSON(Message{
		Type:         TypeRecordingResult,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		Format:       payload.Format,
		SampleRate:   payload.SampleRate,
		IsLast:       true,
		Metadata:     compactMetadata(metadata),
	}); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入录音结果失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.String("recordingId", payload.RecordingID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) handleErrorEvent(ctx context.Context, event model.DomainEvent) {
	payload, err := pbxevents.DecodePayload[pbxevents.ErrorPayload](event)
	if err != nil || !a.isActive(payload.ConnectionID) {
		return
	}
	if err := a.conn.writeJSON(Message{
		Type:         TypeError,
		RequestID:    payload.RequestID,
		ConnectionID: payload.ConnectionID,
		CallID:       payload.CallID,
		UserID:       payload.UserID,
		Error:        payload.Error,
		Metadata:     payload.Metadata,
	}); err != nil {
		slog.WarnContext(ctx, "PBX event adapter 写入错误结果失败",
			slog.String("connectionId", payload.ConnectionID),
			slog.Any("error", err),
		)
	}
}

func (a *eventAdapter) isActive(connectionID string) bool {
	if connectionID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.active[connectionID]
	return ok
}
