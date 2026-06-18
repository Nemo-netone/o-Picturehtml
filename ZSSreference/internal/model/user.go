//  用户模型
package model

import "time"

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type User struct {
	Metadata
	ID           string     `json:"id" validate:"required"`
	TenantID     string     `json:"tenantId" validate:"required"`
	Email        string     `json:"email" validate:"required,email"`
	DisplayName  string     `json:"displayName" validate:"omitempty"`
	PasswordHash string     `json:"passwordHash,omitempty" validate:"omitempty"`
	Status       UserStatus `json:"status" validate:"required"`
	Roles        []Role     `json:"roles,omitempty" validate:"omitempty"`
}

type Role struct {
	ID          string       `json:"id" validate:"required"`
	TenantID    string       `json:"tenantId" validate:"omitempty"`
	Name        string       `json:"name" validate:"required"`
	Permissions []Permission `json:"permissions,omitempty" validate:"omitempty"`
}

type Permission struct {
	Action   string `json:"action" validate:"required"`
	Resource string `json:"resource" validate:"required"`
}

type APIKey struct {
	Metadata
	ID         string    `json:"id" validate:"required"`
	TenantID   string    `json:"tenantId" validate:"required"`
	UserID     string    `json:"userId" validate:"required"`
	Name       string    `json:"name" validate:"required"`
	HashedKey  string    `json:"hashedKey" validate:"required"`
	Scopes     []string  `json:"scopes,omitempty" validate:"omitempty"`
	ExpiresAt  time.Time `json:"expiresAt,omitempty" validate:"omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty" validate:"omitempty"`
}

