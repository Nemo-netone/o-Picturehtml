//  通用基础类型定义
package model

import "time"

type Metadata struct {
	Version   int64     `json:"version" validate:"gte=0"`
	CreatedAt time.Time `json:"createdAt" validate:"omitempty"`
	UpdatedAt time.Time `json:"updatedAt" validate:"omitempty"`
}

