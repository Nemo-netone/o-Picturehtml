// AI结果模型：ASRResult(识别结果)+TranslationResult(翻译结果)+AIState(状态)
package model

import "fmt"

type AIState string

const (
	AIStateIdle          AIState = "idle"
	AIStateUserSpeaking  AIState = "user_speaking"
	AIStateASRFinalizing AIState = "asr_finalizing"
	AIStateAgentThinking AIState = "agent_thinking"
	AIStateAISpeaking    AIState = "ai_speaking"
	AIStateBargeIn       AIState = "barge_in"
	AIStateStopped       AIState = "stopped"
)

type AIPolicy struct {
	Metadata
	ID        string            `json:"id" validate:"required"`
	TenantID  string            `json:"tenantId" validate:"required"`
	Language  string            `json:"language" validate:"required"`
	VAD       VADSettings       `json:"vad" validate:"omitempty"`
	ASR       ProviderSettings  `json:"asr" validate:"omitempty"`
	Agent     ProviderSettings  `json:"agent" validate:"omitempty"`
	TTS       ProviderSettings  `json:"tts" validate:"omitempty"`
	Tools     []string          `json:"tools,omitempty" validate:"omitempty"`
	Prompt    string            `json:"prompt,omitempty" validate:"omitempty"`
	Variables map[string]string `json:"variables,omitempty" validate:"omitempty"`
	Enabled   bool              `json:"enabled"`
}

type VADSettings struct {
	SpeechThreshold        float64 `json:"speechThreshold" validate:"gte=0,lte=1"`
	SilenceDurationMs      int     `json:"silenceDurationMs" validate:"gte=0"`
	SpeechMinDurationMs    int     `json:"speechMinDurationMs" validate:"gte=0"`
	BargeInEnergyThreshold float64 `json:"bargeInEnergyThreshold" validate:"omitempty"`
	BargeInDurationMs      int     `json:"bargeInDurationMs" validate:"gte=0"`
}

type ProviderSettings struct {
	Provider string            `json:"provider" validate:"omitempty"`
	Model    string            `json:"model" validate:"omitempty"`
	Voice    string            `json:"voice,omitempty" validate:"omitempty"`
	Params   map[string]string `json:"params,omitempty" validate:"omitempty"`
}

type AIPipeline struct {
	Metadata
	CallID      string  `json:"callId" validate:"required"`
	TenantID    string  `json:"tenantId,omitempty" validate:"omitempty"`
	PolicyID    string  `json:"policyId,omitempty" validate:"omitempty"`
	VAD         string  `json:"vad,omitempty" validate:"omitempty"`
	ASR         string  `json:"asr,omitempty" validate:"omitempty"`
	Agent       string  `json:"agent,omitempty" validate:"omitempty"`
	TTS         string  `json:"tts,omitempty" validate:"omitempty"`
	State       AIState `json:"state" validate:"required"`
	UtteranceID string  `json:"utteranceId,omitempty" validate:"omitempty"`
}

type VADEvent struct {
	CallID      string  `json:"callId" validate:"required"`
	UtteranceID string  `json:"utteranceId" validate:"required"`
	Event       string  `json:"event" validate:"required"`
	Track       string  `json:"track,omitempty" validate:"omitempty"`
	Confidence  float64 `json:"confidence" validate:"gte=0,lte=1"`
	Energy      float64 `json:"energy" validate:"omitempty"`
	TimestampMS int64   `json:"timestampMs" validate:"required"`
}

type ASRResult struct {
	CallID      string  `json:"callId" validate:"required"`
	UtteranceID string  `json:"utteranceId,omitempty" validate:"omitempty"`
	Text        string  `json:"text" validate:"required"`
	IsFinal     bool    `json:"isFinal"`
	Confidence  float64 `json:"confidence" validate:"gte=0,lte=1"`
	Language    string  `json:"language,omitempty" validate:"omitempty"`
	StartMs     int64   `json:"startMs,omitempty" validate:"omitempty"`
	EndMs       int64   `json:"endMs,omitempty" validate:"omitempty"`
}

// TranslationResult 表示一条字幕翻译结果，用于同声传译的双语字幕与 commit/revise 渲染。
// 同一句话的 TMT 草稿与 DeepSeek/LLM final 共享 UtteranceID，前端据此就地更新。
type TranslationResult struct {
	CallID      string `json:"callId" validate:"required"`
	UtteranceID string `json:"utteranceId,omitempty" validate:"omitempty"`
	SourceText  string `json:"sourceText" validate:"required"`
	Text        string `json:"text" validate:"required"`
	IsFinal     bool   `json:"isFinal"`
	Engine      string `json:"engine,omitempty" validate:"omitempty"`
	Revised     bool   `json:"revised"`
	Language    string `json:"language,omitempty" validate:"omitempty"`
}

type AgentEvent struct {
	CallID      string `json:"callId" validate:"required"`
	UtteranceID string `json:"utteranceId,omitempty" validate:"omitempty"`
	Event       string `json:"event" validate:"required"`
	Text        string `json:"text,omitempty" validate:"omitempty"`
	ToolName    string `json:"toolName,omitempty" validate:"omitempty"`
	TimestampMS int64  `json:"timestampMs" validate:"required"`
}

type TTSChunk struct {
	CallID      string `json:"callId" validate:"required"`
	UtteranceID string `json:"utteranceId,omitempty" validate:"omitempty"`
	Audio       []byte `json:"audio" validate:"required"`
	Format      string `json:"format" validate:"required"`
	SampleRate  int    `json:"sampleRate" validate:"required"`
	Sequence    int    `json:"sequence" validate:"gte=0"`
	IsLast      bool   `json:"isLast"`
}

// CanTransitionAIState 判断 AI 状态转移是否合法。
func CanTransitionAIState(from, to AIState) bool {
	if from == to {
		return true
	}

	transitions := map[AIState]map[AIState]bool{
		AIStateIdle: {
			AIStateUserSpeaking:  true,
			AIStateAgentThinking: true,
			AIStateStopped:       true,
		},
		AIStateUserSpeaking: {
			AIStateASRFinalizing: true,
			AIStateIdle:          true,
			AIStateStopped:       true,
		},
		AIStateASRFinalizing: {
			AIStateAgentThinking: true,
			AIStateIdle:          true,
			AIStateStopped:       true,
		},
		AIStateAgentThinking: {
			AIStateAISpeaking: true,
			AIStateIdle:       true,
			AIStateStopped:    true,
		},
		AIStateAISpeaking: {
			AIStateIdle:    true,
			AIStateBargeIn: true,
			AIStateStopped: true,
		},
		AIStateBargeIn: {
			AIStateUserSpeaking: true,
			AIStateStopped:      true,
		},
	}

	return transitions[from][to]
}

// ValidateAITransition 校验 AI 状态转移，不合法时返回错误信息。
func ValidateAITransition(from, to AIState) error {
	if CanTransitionAIState(from, to) {
		return nil
	}

	return fmt.Errorf("invalid ai state transition: %s -> %s", from, to)
}
