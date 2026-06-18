// Package client 是 SimulSpeak 的 PBX 节点客户端库（SDK）。
//
// 本文件实现 PBX 节点池(NodePool)：维护可用 PBX 媒体节点的本地缓存，通过 etcd watch 机制
// 实时同步节点上下线事件，提供按策略选取节点的能力。api-server 通过节点池为新同传会话
// 选择合适的 pbx-node 承载 WebRTC 媒体处理。
package client

import (
	"context"
	"strings"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// NodePool 是 PBX 媒体节点池：缓存可用节点列表，监听节点变更事件，提供按策略选取节点的能力。
// 内部为每种负载均衡策略缓存一个 Balancer 实例，避免重复创建。
type NodePool struct {
	registry *registry.Registry // etcd 注册中心，用于查询和监听节点
	nodeType model.NodeType     // 节点类型过滤（当前固定为 NodeTypeMedia）

	mu        sync.RWMutex         // 保护 nodes 和 balancers 的并发读写
	nodes     map[string]*model.Node // 节点 ID → 节点信息的本地缓存
	balancers map[string]Balancer    // 策略名 → 均衡器实例的缓存
}

// NewNodePool 创建节点池并绑定到指定的注册中心和节点类型。
func NewNodePool(reg *registry.Registry, nodeType model.NodeType) *NodePool {
	return &NodePool{
		registry:  reg,
		nodeType:  nodeType,
		nodes:     map[string]*model.Node{},
		balancers: map[string]Balancer{},
	}
}

// Start 启动节点池：首次全量刷新节点列表，然后启动后台 goroutine 监听节点变更事件。
// 必须在 Pick 调用之前调用，否则节点列表为空。
func (p *NodePool) Start(ctx context.Context) error {
	// 首次全量拉取可用节点
	if err := p.Refresh(ctx); err != nil {
		return err
	}
	// 订阅节点变更事件（上线/下线）
	events := p.registry.WatchNodes(ctx, p.nodeType)
	go func() {
		for event := range events {
			p.applyEvent(event)
		}
	}()
	return nil
}

// Refresh 从注册中心全量刷新节点列表，替换当前缓存。
func (p *NodePool) Refresh(ctx context.Context) error {
	nodes, err := p.registry.ListNodes(ctx, p.nodeType)
	if err != nil {
		return err
	}
	next := make(map[string]*model.Node, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		next[node.ID] = cloneNode(node)
	}
	p.mu.Lock()
	p.nodes = next
	p.mu.Unlock()
	return nil
}

// Pick 根据负载均衡策略和 key 从可用节点中选取一个节点。
// 常用 key：同传会话的 callID（一致性哈希场景）或空字符串（轮询/最少负载场景）。
func (p *NodePool) Pick(policy, key string) (*model.Node, error) {
	nodes := p.Nodes()
	balancer := p.balancer(policy)
	return balancer.Pick(nodes, key)
}

// Nodes 返回当前缓存的全部节点快照（深拷贝，防止调用方修改内部状态）。
func (p *NodePool) Nodes() []*model.Node {
	p.mu.RLock()
	defer p.mu.RUnlock()
	nodes := make([]*model.Node, 0, len(p.nodes))
	for _, node := range p.nodes {
		nodes = append(nodes, cloneNode(node))
	}
	return nodes
}

// applyEvent 处理节点变更事件：上线时添加/更新节点缓存，下线时删除。
func (p *NodePool) applyEvent(event registry.NodeEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch event.Type {
	case registry.NodeEventUp:
		if event.Node != nil {
			p.nodes[event.Node.ID] = cloneNode(event.Node)
		}
	case registry.NodeEventDown:
		delete(p.nodes, nodeIDFromKey(p.nodeType, event.Key))
	}
}

// balancer 获取或创建指定策略的负载均衡器实例（懒加载+缓存）。
func (p *NodePool) balancer(policy string) Balancer {
	normalized := normalizePolicy(policy)
	p.mu.Lock()
	defer p.mu.Unlock()
	balancer := p.balancers[normalized]
	if balancer == nil {
		balancer = BalancerFor(normalized)
		p.balancers[normalized] = balancer
	}
	return balancer
}

// normalizePolicy 归一化策略名称：未知策略默认返回 least_load。
func normalizePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case string(PolicyRoundRobin):
		return string(PolicyRoundRobin)
	case string(PolicyConsistentHash):
		return string(PolicyConsistentHash)
	default:
		return string(PolicyLeastLoad)
	}
}

// nodeIDFromKey 从 etcd key 中提取节点 ID（去掉类型前缀）。
func nodeIDFromKey(nodeType model.NodeType, key string) string {
	return strings.TrimPrefix(key, registry.NodePrefix(nodeType))
}
