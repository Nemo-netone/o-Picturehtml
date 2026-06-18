// PBX事件定义
package events

import (
	"encoding/json"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

const Topic = "pbx.events"

type WebRTCAnswerPayload struct {
	RequestID    string            `json:"requestId,omitempty"`
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	SDP          string            `json:"sdp,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ICECandidatePayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	Candidate    string            `json:"candidate,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ASRResultPayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	UtteranceID  string            `json:"utteranceId,omitempty"`
	Text         string            `json:"text,omitempty"`
	IsFinal      bool              `json:"isFinal,omitempty"`
	Confidence   float64           `json:"confidence,omitempty"`
	Language     string            `json:"language,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type TranslationResultPayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	UtteranceID  string            `json:"utteranceId,omitempty"`
	SourceText   string            `json:"sourceText,omitempty"`
	Text         string            `json:"text,omitempty"`
	IsFinal      bool              `json:"isFinal,omitempty"`
	Engine       string            `json:"engine,omitempty"`
	Revised      bool              `json:"revised"`
	Language     string            `json:"language,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type TTSResultPayload struct {
	RequestID    string            `json:"requestId,omitempty"`
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	UtteranceID  string            `json:"utteranceId,omitempty"`
	Text         string            `json:"text,omitempty"`
	Format       string            `json:"format,omitempty"`
	SampleRate   int               `json:"sampleRate,omitempty"`
	IsLast       bool              `json:"isLast,omitempty"`
	Voice        string            `json:"voice,omitempty"`
	Language     string            `json:"language,omitempty"`
	Sequence     int               `json:"sequence,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type RecordingResultPayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	RecordingID  string            `json:"recordingId,omitempty"`
	ObjectKey    string            `json:"objectKey,omitempty"`
	Checksum     string            `json:"checksum,omitempty"`
	Size         int64             `json:"size,omitempty"`
	SampleRate   int               `json:"sampleRate,omitempty"`
	Format       string            `json:"format,omitempty"`
	StartedAt    string            `json:"startedAt,omitempty"`
	StoppedAt    string            `json:"stoppedAt,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ErrorPayload struct {
	RequestID    string            `json:"requestId,omitempty"`
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	Error        string            `json:"error,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type SessionClosedPayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type StatePayload struct {
	ConnectionID string            `json:"connectionId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	State        string            `json:"state,omitempty"`
	Kind         string            `json:"kind,omitempty"`
	Codec        string            `json:"codec,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func NewWebRTCAnswerEvent(payload WebRTCAnswerPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXWebRTCAnswerCreated, payload.CallID, "", payloadMap(payload))
}

func NewICECandidateEvent(payload ICECandidatePayload) model.DomainEvent {
	return newEvent(model.EventTypePBXICECandidate, payload.CallID, "", payloadMap(payload))
}

func NewASRResultEvent(payload ASRResultPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXASRResult, payload.CallID, payload.UtteranceID, payloadMap(payload))
}

func NewTranslationResultEvent(payload TranslationResultPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXTranslationResult, payload.CallID, payload.UtteranceID, payloadMap(payload))
}

func NewTTSResultEvent(payload TTSResultPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXTTSResult, payload.CallID, payload.UtteranceID, payloadMap(payload))
}

func NewRecordingResultEvent(payload RecordingResultPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXRecordingResult, payload.CallID, "", payloadMap(payload))
}

func NewErrorEvent(payload ErrorPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXError, payload.CallID, "", payloadMap(payload))
}

func NewSessionClosedEvent(payload SessionClosedPayload) model.DomainEvent {
	return newEvent(model.EventTypePBXSessionClosed, payload.CallID, "", payloadMap(payload))
}

func NewConnectionStateEvent(payload StatePayload) model.DomainEvent {
	return newEvent(model.EventTypePBXWebRTCConnectionState, payload.CallID, "", payloadMap(payload))
}

func NewICEConnectionStateEvent(payload StatePayload) model.DomainEvent {
	return newEvent(model.EventTypePBXICEConnectionState, payload.CallID, "", payloadMap(payload))
}

func NewTrackReceivedEvent(payload StatePayload) model.DomainEvent {
	return newEvent(model.EventTypePBXTrackReceived, payload.CallID, "", payloadMap(payload))
}

func DecodePayload[T any](event model.DomainEvent) (T, error) {
	var out T
	data, err := json.Marshal(event.Payload)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func newEvent(eventType model.EventType, callID, utteranceID string, payload map[string]any) model.DomainEvent {
	return model.DomainEvent{
		ID:          idgen.EventID(),
		CallID:      callID,
		UtteranceID: utteranceID,
		Type:        eventType,
		Source:      model.EventSourceMediaNode,
		Payload:     payload,
		TimestampMS: time.Now().UnixMilli(),
	}
}

func payloadMap(payload any) map[string]any {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
