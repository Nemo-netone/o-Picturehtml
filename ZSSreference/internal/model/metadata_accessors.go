//  元数据访问器
package model

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *Tenant) GetMetadata() *Metadata {
	return &m.Metadata
}

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *Extension) GetMetadata() *Metadata {
	return &m.Metadata
}

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *SIPTrunk) GetMetadata() *Metadata {
	return &m.Metadata
}

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *Route) GetMetadata() *Metadata {
	return &m.Metadata
}

// GetMetadata 返回元数据指针（实现 versionedModel 接口）。
func (m *AIPolicy) GetMetadata() *Metadata {
	return &m.Metadata
}

