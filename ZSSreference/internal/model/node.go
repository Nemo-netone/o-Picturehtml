// PBX节点模型：节点ID+类型+状态+负载+能力
package model

import "time"

type NodeType string

const (
	NodeTypeControl   NodeType = "control"
	NodeTypeSignaling NodeType = "signaling"
	NodeTypeMedia     NodeType = "media"
	NodeTypeTurn      NodeType = "turn"
	NodeTypeAIVAD     NodeType = "ai_vad"
	NodeTypeAIASR     NodeType = "ai_asr"
	NodeTypeAITMT     NodeType = "ai_tmt"
	NodeTypeAITTS     NodeType = "ai_tts"
	NodeTypeAIAgent   NodeType = "ai_agent"
	NodeTypeWorker    NodeType = "worker"
)

type NodeStatus string

const (
	NodeStatusUp       NodeStatus = "up"
	NodeStatusDown     NodeStatus = "down"
	NodeStatusDraining NodeStatus = "draining"
	NodeStatusSuspect  NodeStatus = "suspect"
)

type Node struct {
	Metadata
	ID           string            `json:"id" validate:"required"`
	Type         NodeType          `json:"type" validate:"required"`
	Endpoint     string            `json:"endpoint" validate:"required"`
	Zone         string            `json:"zone" validate:"omitempty"`
	Status       NodeStatus        `json:"status" validate:"required"`
	Weight       int               `json:"weight" validate:"gte=0"`
	MaxCalls     int               `json:"maxCalls" validate:"gte=0"`
	CurrentCalls int               `json:"currentCalls" validate:"gte=0"`
	Version      string            `json:"nodeVersion" validate:"omitempty"`
	StartedAt    time.Time         `json:"startedAt" validate:"omitempty"`
	LeaseID      int64             `json:"leaseId,omitempty" validate:"omitempty"`
	Capabilities []string          `json:"capabilities,omitempty" validate:"omitempty"`
	Labels       map[string]string `json:"labels,omitempty" validate:"omitempty"`
}

// CanTransitionNodeStatus 判断节点状态转移是否合法。
func CanTransitionNodeStatus(from, to NodeStatus) bool {
	if from == to {
		return true
	}

	switch from {
	case NodeStatusUp:
		return to == NodeStatusDraining || to == NodeStatusSuspect || to == NodeStatusDown
	case NodeStatusDraining:
		return to == NodeStatusUp || to == NodeStatusDown
	case NodeStatusSuspect:
		return to == NodeStatusUp || to == NodeStatusDown || to == NodeStatusDraining
	case NodeStatusDown:
		return to == NodeStatusUp
	default:
		return false
	}
}

type CapabilityType string

const (
	CapabilityTypeVAD   CapabilityType = "vad"
	CapabilityTypeASR   CapabilityType = "asr"
	CapabilityTypeTMT   CapabilityType = "tmt"
	CapabilityTypeTTS   CapabilityType = "tts"
	CapabilityTypeAgent CapabilityType = "agent"
)

type Capability struct {
	Metadata
	ID                 string            `json:"id" validate:"required"`
	Type               CapabilityType    `json:"type" validate:"required"`
	Protocol           string            `json:"protocol" validate:"required"`
	Endpoint           string            `json:"endpoint" validate:"omitempty"`
	Languages          []string          `json:"languages,omitempty" validate:"omitempty"`
	Models             []string          `json:"models,omitempty" validate:"omitempty"`
	Zone               string            `json:"zone,omitempty" validate:"omitempty"`
	Tenants            []string          `json:"tenants,omitempty" validate:"omitempty"`
	Streaming          bool              `json:"streaming" validate:"omitempty"`
	MaxConcurrency     int               `json:"maxConcurrency" validate:"gte=0"`
	CurrentConcurrency int               `json:"currentConcurrency" validate:"gte=0"`
	Labels             map[string]string `json:"labels,omitempty" validate:"omitempty"`
	ProviderConfig     *ProviderConfig   `json:"providerConfig,omitempty" validate:"omitempty"`
}

type ProviderConfig struct {
	Provider string            `json:"provider,omitempty" validate:"omitempty"`
	Endpoint string            `json:"endpoint,omitempty" validate:"omitempty"`
	Params   map[string]string `json:"params,omitempty" validate:"omitempty"`
	Secrets  map[string]string `json:"secrets,omitempty" validate:"omitempty"`
}

type CapabilitySelector struct {
	Type     CapabilityType `json:"type,omitempty"`
	Protocol string         `json:"protocol,omitempty"`
	Language string         `json:"language,omitempty"`
	Model    string         `json:"model,omitempty"`
	Zone     string         `json:"zone,omitempty"`
	TenantID string         `json:"tenantId,omitempty"`
}

// Matches 判断 capability 是否满足选择器的所有非空字段。
func (c Capability) Matches(selector CapabilitySelector) bool {
	if selector.Type != "" && c.Type != selector.Type {
		return false
	}
	if selector.Protocol != "" && c.Protocol != selector.Protocol {
		return false
	}
	if selector.Language != "" && !contains(c.Languages, selector.Language) {
		return false
	}
	if selector.Model != "" && !contains(c.Models, selector.Model) {
		return false
	}
	if selector.Zone != "" && c.Zone != "" && c.Zone != selector.Zone {
		return false
	}
	if selector.TenantID != "" && len(c.Tenants) > 0 && !contains(c.Tenants, selector.TenantID) {
		return false
	}
	if c.MaxConcurrency > 0 && c.CurrentConcurrency >= c.MaxConcurrency {
		return false
	}

	return true
}

// contains 检查目标是否在切片中。
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
