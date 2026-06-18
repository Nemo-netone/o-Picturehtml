//  租户模型
package model

type TenantTier string

const (
	TenantTierFree       TenantTier = "free"
	TenantTierPro        TenantTier = "pro"
	TenantTierEnterprise TenantTier = "enterprise"
)

type TenantStatus string

const (
	TenantStatusActive   TenantStatus = "active"
	TenantStatusDisabled TenantStatus = "disabled"
)

type Tenant struct {
	Metadata
	ID       string         `json:"id" validate:"required"`
	Name     string         `json:"name" validate:"required"`
	Tier     TenantTier     `json:"tier" validate:"required"`
	Status   TenantStatus   `json:"status" validate:"required"`
	Settings TenantSettings `json:"settings" validate:"omitempty"`
}

type TenantSettings struct {
	MaxCalls      int               `json:"maxCalls" validate:"gte=0"`
	MaxAIWorkers  int               `json:"maxAIWorkers" validate:"gte=0"`
	Features      []string          `json:"features,omitempty" validate:"omitempty"`
	RetentionDays int               `json:"retentionDays" validate:"gte=0"`
	Extra         map[string]string `json:"extra,omitempty" validate:"omitempty"`
}

