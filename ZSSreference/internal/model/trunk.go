//  中继线路模型
package model

type SIPTrunkStatus string

const (
	SIPTrunkStatusEnabled  SIPTrunkStatus = "enabled"
	SIPTrunkStatusDisabled SIPTrunkStatus = "disabled"
)

type SIPTrunk struct {
	Metadata
	ID            string            `json:"id" validate:"required"`
	TenantID      string            `json:"tenantId" validate:"required"`
	Provider      string            `json:"provider" validate:"required"`
	Endpoint      string            `json:"endpoint" validate:"required"`
	Transport     string            `json:"transport" validate:"required"`
	Codecs        []string          `json:"codecs,omitempty" validate:"omitempty"`
	Credential    Credential        `json:"credential" validate:"omitempty"`
	InboundRules  []InboundRule     `json:"inboundRules,omitempty" validate:"omitempty"`
	OutboundRules []OutboundRule    `json:"outboundRules,omitempty" validate:"omitempty"`
	Status        SIPTrunkStatus    `json:"status" validate:"required"`
	Labels        map[string]string `json:"labels,omitempty" validate:"omitempty"`
}

type Credential struct {
	Username  string `json:"username,omitempty" validate:"omitempty"`
	SecretRef string `json:"secretRef,omitempty" validate:"omitempty"`
	Realm     string `json:"realm,omitempty" validate:"omitempty"`
}

type InboundRule struct {
	ID      string `json:"id" validate:"required"`
	DID     string `json:"did" validate:"required"`
	RouteID string `json:"routeId" validate:"required"`
	Enabled bool   `json:"enabled"`
}

type OutboundRule struct {
	ID       string `json:"id" validate:"required"`
	Prefix   string `json:"prefix" validate:"required"`
	Rewrite  string `json:"rewrite,omitempty" validate:"omitempty"`
	Priority int    `json:"priority" validate:"gte=0"`
	Enabled  bool   `json:"enabled"`
}

