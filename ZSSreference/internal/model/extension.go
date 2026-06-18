//  分机模型
package model

import "time"

type ExtensionStatus string

const (
	ExtensionStatusEnabled  ExtensionStatus = "enabled"
	ExtensionStatusDisabled ExtensionStatus = "disabled"
)

type PresenceStatus string

const (
	PresenceStatusOnline  PresenceStatus = "online"
	PresenceStatusOffline PresenceStatus = "offline"
	PresenceStatusRinging PresenceStatus = "ringing"
	PresenceStatusInCall  PresenceStatus = "in_call"
)

type Extension struct {
	Metadata
	ID            string          `json:"id" validate:"required"`
	TenantID      string          `json:"tenantId" validate:"required"`
	Extension     string          `json:"extension" validate:"required"`
	DisplayName   string          `json:"displayName" validate:"omitempty"`
	SecretHash    string          `json:"secretHash,omitempty" validate:"omitempty"`
	Status        ExtensionStatus `json:"status" validate:"required"`
	DevicePolicy  string          `json:"devicePolicy,omitempty" validate:"omitempty"`
	DeviceBinding []DeviceBinding `json:"deviceBinding,omitempty" validate:"omitempty"`
	Registrations []Registration  `json:"registrations,omitempty" validate:"omitempty"`
	Presence      *Presence       `json:"presence,omitempty" validate:"omitempty"`
}

type Presence struct {
	Extension    string         `json:"extension" validate:"required"`
	TenantID     string         `json:"tenantId" validate:"required"`
	GatewayID    string         `json:"gatewayId" validate:"required"`
	Status       PresenceStatus `json:"status" validate:"required"`
	ConnectionID string         `json:"connectionId" validate:"required"`
	UpdatedAt    time.Time      `json:"updatedAt" validate:"required"`
}

type DeviceBinding struct {
	ID         string    `json:"id" validate:"required"`
	UserAgent  string    `json:"userAgent,omitempty" validate:"omitempty"`
	RemoteAddr string    `json:"remoteAddr,omitempty" validate:"omitempty"`
	BoundAt    time.Time `json:"boundAt" validate:"omitempty"`
}

type Registration struct {
	ID           string    `json:"id" validate:"required"`
	ContactURI   string    `json:"contactUri" validate:"required"`
	Transport    string    `json:"transport" validate:"required"`
	RegisteredAt time.Time `json:"registeredAt" validate:"omitempty"`
	ExpiresAt    time.Time `json:"expiresAt" validate:"omitempty"`
}

