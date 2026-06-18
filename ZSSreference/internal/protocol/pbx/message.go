// PBX协议消息类型定义：webrtc_offer/answer/ice/asr_result/translation_result/tts_command等
package pbxprotocol

const (
	TypeWebRTCOffer       = "webrtc_offer"
	TypeWebRTCAnswer      = "webrtc_answer"
	TypeICE               = "ice"
	TypeASRResult         = "asr_result"
	TypeTranslationResult = "translation_result"
	TypeTTSCommand        = "tts_command"
	TypeTTSResult         = "tts_result"
	TypeRecordingResult   = "recording_result"
	TypeCloseSession      = "close_session"
	TypeError             = "error"
	TypePing              = "ping"
	TypePong              = "pong"
)

type Message struct {
	Type         string            `json:"type"`
	RequestID    string            `json:"requestId,omitempty"`
	ConnectionID string            `json:"connectionId,omitempty"`
	TenantID     string            `json:"tenantId,omitempty"`
	CallID       string            `json:"callId,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	SDP          string            `json:"sdp,omitempty"`
	Candidate    string            `json:"candidate,omitempty"`
	Text         string            `json:"text,omitempty"`
	UtteranceID  string            `json:"utteranceId,omitempty"`
	SourceText   string            `json:"sourceText,omitempty"`
	Engine       string            `json:"engine,omitempty"`
	Revised      bool              `json:"revised"`
	IsFinal      bool              `json:"isFinal,omitempty"`
	Confidence   float64           `json:"confidence,omitempty"`
	Language     string            `json:"language,omitempty"`
	Voice        string            `json:"voice,omitempty"`
	Format       string            `json:"format,omitempty"`
	SampleRate   int               `json:"sampleRate,omitempty"`
	Sequence     int               `json:"sequence,omitempty"`
	IsLast       bool              `json:"isLast,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Error        string            `json:"error,omitempty"`
}
