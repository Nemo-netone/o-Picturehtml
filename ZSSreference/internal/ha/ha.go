//  高可用(HA)：主备选举与故障转移
package ha

import (
	"context"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
)

type Election struct {
	mu      sync.Mutex
	leaders map[string]string
}

// NewElection 创建简单的内存选主器。
func NewElection() *Election {
	return &Election{leaders: map[string]string{}}
}

// TryAcquire 尝试获取 leader 角色，group 已有 leader 时返回 false。
func (e *Election) TryAcquire(group, candidate string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.leaders[group] != "" {
		return false
	}
	e.leaders[group] = candidate
	return true
}

// Release 释放 leader 角色（仅当前 leader 可释放）。
func (e *Election) Release(group, candidate string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.leaders[group] == candidate {
		delete(e.leaders, group)
	}
}

type FaultDetector struct {
	registry *registry.Registry
	sessions *session.Manager
}

// NewFaultDetector 创建故障检测器。
func NewFaultDetector(registry *registry.Registry, sessions *session.Manager) *FaultDetector {
	return &FaultDetector{registry: registry, sessions: sessions}
}

// OnNodeDown 处理节点故障：将该节点所有 connected 通话标记为 suspect，然后注销节点。
func (d *FaultDetector) OnNodeDown(ctx context.Context, nodeType model.NodeType, nodeID string) error {
	sessions, err := d.sessions.ListSessions(ctx, session.Filter{NodeID: nodeID})
	if err != nil {
		return err
	}
	for _, call := range sessions {
		if call.State == model.CallStateConnected {
			_ = d.sessions.MarkSuspect(ctx, call.ID)
		}
	}
	return d.registry.Deregister(ctx, nodeType, nodeID)
}

// OnNodeRecover 处理节点恢复：重新注册该节点。
func (d *FaultDetector) OnNodeRecover(ctx context.Context, node *model.Node) error {
	return d.registry.Register(ctx, node)
}

type DrainController struct {
	registry *registry.Registry
}

// NewDrainController 创建优雅摘除控制器。
func NewDrainController(registry *registry.Registry) *DrainController {
	return &DrainController{registry: registry}
}

// Start 将节点状态设为 draining 并重新注册（通知注册中心不再分配新通话）。
func (d *DrainController) Start(ctx context.Context, nodeType model.NodeType, nodeID string) error {
	node, err := d.registry.GetNode(ctx, nodeType, nodeID)
	if err != nil {
		return err
	}
	node.Status = model.NodeStatusDraining
	return d.registry.Register(ctx, node)
}

