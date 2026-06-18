//  通话会话模型：CallSession+CallState状态机
package model

import "fmt"

type CallState string

const (
	CallStateIdle       CallState = "idle"
	CallStateRinging    CallState = "ringing"
	CallStateConnected  CallState = "connected"
	CallStateEnded      CallState = "ended"
	CallStateRejected   CallState = "rejected"
	CallStateFailed     CallState = "failed"
	CallStateSuspect    CallState = "suspect"
	CallStateLost       CallState = "lost"
	CallStateRecovering CallState = "recovering"
)

type MediaState string

const (
	MediaStateNew       MediaState = "new"
	MediaStateRinging   MediaState = "ringing"
	MediaStateConnected MediaState = "connected"
	MediaStateHolding   MediaState = "holding"
	MediaStateEnded     MediaState = "ended"
)

type ParticipantRole string

const (
	ParticipantRoleCaller ParticipantRole = "caller"
	ParticipantRoleCallee ParticipantRole = "callee"
	ParticipantRoleAgent  ParticipantRole = "agent"
	ParticipantRoleAI     ParticipantRole = "ai"
)

type CallSession struct {
	Metadata
	ID           string            `json:"id" validate:"required"`
	TenantID     string            `json:"tenantId" validate:"required"`
	Caller       string            `json:"caller" validate:"required"`
	Callee       string            `json:"callee" validate:"required"`
	State        CallState         `json:"state" validate:"required"`
	Media        MediaState        `json:"media" validate:"required"`
	Owner        CallOwner         `json:"owner" validate:"required"`
	GatewayNode  string            `json:"gatewayNode" validate:"required"`
	MediaNode    string            `json:"mediaNode" validate:"required"`
	TurnNode     string            `json:"turnNode,omitempty" validate:"omitempty"`
	Participants []Participant     `json:"participants,omitempty" validate:"omitempty"`
	AIPipeline   *AIPipeline       `json:"aiPipeline,omitempty" validate:"omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty" validate:"omitempty"`
}

type CallOwner struct {
	CallID      string `json:"callId" validate:"required"`
	OwnerNode   string `json:"ownerNode" validate:"required"`
	GatewayNode string `json:"gatewayNode" validate:"required"`
	Epoch       int64  `json:"epoch" validate:"gte=1"`
	LeaseID     int64  `json:"leaseId,omitempty" validate:"omitempty"`
}

type Participant struct {
	ID        string          `json:"id" validate:"required"`
	Extension string          `json:"extension,omitempty" validate:"omitempty"`
	Endpoint  string          `json:"endpoint,omitempty" validate:"omitempty"`
	Role      ParticipantRole `json:"role" validate:"required"`
	State     CallState       `json:"state,omitempty" validate:"omitempty"`
}

// CanTransitionCallState 判断 call 状态转移是否合法。
func CanTransitionCallState(from, to CallState) bool {
	if from == to {
		return true
	}

	transitions := map[CallState]map[CallState]bool{
		CallStateIdle: {
			CallStateRinging: true,
			CallStateFailed:  true,
		},
		CallStateRinging: {
			CallStateConnected: true,
			CallStateRejected:  true,
			CallStateFailed:    true,
			CallStateEnded:     true,
		},
		CallStateConnected: {
			CallStateEnded:   true,
			CallStateFailed:  true,
			CallStateSuspect: true,
		},
		CallStateSuspect: {
			CallStateRecovering: true,
			CallStateLost:       true,
			CallStateEnded:      true,
		},
		CallStateRecovering: {
			CallStateConnected: true,
			CallStateLost:      true,
			CallStateEnded:     true,
		},
	}

	return transitions[from][to]
}

// ValidateCallTransition 校验 call 状态转移，不合法时返回错误信息。
func ValidateCallTransition(from, to CallState) error {
	if CanTransitionCallState(from, to) {
		return nil
	}

	return fmt.Errorf("invalid call state transition: %s -> %s", from, to)
}

