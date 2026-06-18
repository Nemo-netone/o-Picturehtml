//  路由策略模型
package model

type RouteDirection string

const (
	RouteDirectionInbound  RouteDirection = "inbound"
	RouteDirectionOutbound RouteDirection = "outbound"
	RouteDirectionInternal RouteDirection = "internal"
)

type RouteActionType string

const (
	RouteActionExtension RouteActionType = "extension"
	RouteActionTrunk     RouteActionType = "trunk"
	RouteActionRingGroup RouteActionType = "ring_group"
	RouteActionIVR       RouteActionType = "ivr"
	RouteActionAI        RouteActionType = "ai"
	RouteActionReject    RouteActionType = "reject"
)

type Route struct {
	Metadata
	ID         string           `json:"id" validate:"required"`
	TenantID   string           `json:"tenantId" validate:"required"`
	Direction  RouteDirection   `json:"direction" validate:"required"`
	Priority   int              `json:"priority" validate:"gte=0"`
	Conditions []RouteCondition `json:"conditions,omitempty" validate:"omitempty"`
	Actions    []RouteAction    `json:"actions,omitempty" validate:"omitempty"`
	Enabled    bool             `json:"enabled"`
}

type RouteCondition struct {
	Field    string   `json:"field" validate:"required"`
	Operator string   `json:"operator" validate:"required"`
	Values   []string `json:"values" validate:"required"`
}

type RouteAction struct {
	Type     RouteActionType   `json:"type" validate:"required"`
	TargetID string            `json:"targetId,omitempty" validate:"omitempty"`
	Params   map[string]string `json:"params,omitempty" validate:"omitempty"`
}

