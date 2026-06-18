//  ICE候选模型
package model

type ICEConfig struct {
	Metadata
	TenantID      string      `json:"tenantId" validate:"required"`
	STUNServers   []ICEServer `json:"stunServers,omitempty" validate:"omitempty"`
	TURNServers   []ICEServer `json:"turnServers,omitempty" validate:"omitempty"`
	Policy        string      `json:"policy,omitempty" validate:"omitempty"`
	CredentialTTL int         `json:"credentialTtl" validate:"gte=0"`
}

type ICEServer struct {
	URLs       []string `json:"urls" validate:"required"`
	Username   string   `json:"username,omitempty" validate:"omitempty"`
	Credential string   `json:"credential,omitempty" validate:"omitempty"`
}

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *ICEConfig) GetMetadata() *Metadata {
	return &m.Metadata
}

