// 事件模型定义
package model

type EventType string

const (
	EventTypeNodeChanged   EventType = "node_changed"
	EventTypeConfigChanged EventType = "config_changed"
	EventTypeCallChanged   EventType = "call_changed"
	EventTypeSignal        EventType = "signal"
	EventTypeMedia         EventType = "media"
	EventTypeAI            EventType = "ai"
	EventTypeAudit         EventType = "audit"

	EventTypePBXWebRTCAnswerCreated   EventType = "pbx.webrtc.answer.created"
	EventTypePBXWebRTCConnectionState EventType = "pbx.webrtc.connection.state"
	EventTypePBXICECandidate          EventType = "pbx.ice.local.candidate"
	EventTypePBXICEConnectionState    EventType = "pbx.ice.connection.state"
	EventTypePBXTrackReceived         EventType = "pbx.track.received"
	EventTypePBXASRResult             EventType = "pbx.asr.result"
	EventTypePBXTranslationResult     EventType = "pbx.translation.result"
	EventTypePBXTTSResult             EventType = "pbx.tts.result"
	EventTypePBXRecordingResult       EventType = "pbx.recording.result"
	EventTypePBXError                 EventType = "pbx.error"
	EventTypePBXSessionClosed         EventType = "pbx.session.closed"
)

type EventSource string

const (
	EventSourceAPIServer        EventSource = "api-server"
	EventSourceSignalingGateway EventSource = "signaling-gateway"
	EventSourceMediaNode        EventSource = "media-node"
	EventSourceAIWorker         EventSource = "ai-worker"
	EventSourceAdmin            EventSource = "admin"
)

type DomainEvent struct {
	Metadata
	ID          string         `json:"id" validate:"required"`
	TenantID    string         `json:"tenantId,omitempty" validate:"omitempty"`
	CallID      string         `json:"callId,omitempty" validate:"omitempty"`
	UtteranceID string         `json:"utteranceId,omitempty" validate:"omitempty"`
	Type        EventType      `json:"type" validate:"required"`
	Source      EventSource    `json:"source" validate:"required"`
	Payload     map[string]any `json:"payload,omitempty" validate:"omitempty"`
	TimestampMS int64          `json:"timestampMs" validate:"required"`
}
