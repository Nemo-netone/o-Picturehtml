// Package client 是 SimulSpeak 的 PBX 节点客户端库，提供节点池管理、负载均衡、WebSocket 连接、
// AI Provider 配置构建以及 PBX 消息中继等能力。api-server 通过本包发现和连接 pbx-node 媒体节点。
//
// 本文件实现 PBX 节点负载均衡器：支持轮询(round_robin)、最少负载(least_load)、一致性哈希(consistent_hash)
// 三种策略，用于 api-server 为新同传会话选择合适的 pbx-node 承载 WebRTC 媒体处理。
package client

import (
	"errors"
	"hash/crc32"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

// BalancePolicy 负载均衡策略类型（字符串枚举）。
type BalancePolicy string

// 负载均衡策略常量定义
const (
	// PolicyRoundRobin 轮询策略：新会话依次分配到各可用节点，保证负载均匀分布。
	PolicyRoundRobin BalancePolicy = "round_robin"
	// PolicyLeastLoad 最少负载策略：优先选择当前通话数最少的节点（默认策略）。
	PolicyLeastLoad BalancePolicy = "least_load"
	// PolicyConsistentHash 一致性哈希策略：按会话 key 哈希选择节点，同一 key 始终路由到同一节点。
	PolicyConsistentHash BalancePolicy = "consistent_hash"
)

// ErrNoNode 表示当前没有可用的 PBX 节点（所有节点均离线或满载）。
var ErrNoNode = errors.New("no available pbx node")

// Balancer 负载均衡器接口：从可用节点列表中按策略选取一个节点。
type Balancer interface {
	Pick(nodes []*model.Node, key string) (*model.Node, error)
}

// BalancerFor 根据策略名称创建对应的负载均衡器实例。
// 默认策略为 least_load（最少负载），因为它在简单性和负载均匀性之间取得了最佳平衡。
func BalancerFor(policy string) Balancer {
	switch BalancePolicy(strings.ToLower(strings.TrimSpace(policy))) {
	case PolicyRoundRobin:
		return RoundRobin()
	case PolicyConsistentHash:
		return ConsistentHash(64)
	default:
		return LeastLoad()
	}
}

// roundRobinBalancer 轮询负载均衡器：使用原子计数器保证并发安全。
type roundRobinBalancer struct {
	next atomic.Uint64
}

// RoundRobin 创建轮询负载均衡器，每次调用 Pick 返回下一个可用节点。
func RoundRobin() Balancer {
	return &roundRobinBalancer{}
}

// Pick 通过原子自增计数器实现轮询，按顺序选取下一个可用节点。
func (b *roundRobinBalancer) Pick(nodes []*model.Node, key string) (*model.Node, error) {
	// 先过滤出可用节点
	candidates := availableNodes(nodes)
	if len(candidates) == 0 {
		return nil, ErrNoNode
	}
	// 原子自增获取索引，取模后选择节点
	index := b.next.Add(1) - 1
	return cloneNode(candidates[int(index%uint64(len(candidates)))]), nil
}

// leastLoadBalancer 最少负载均衡器：选择当前通话数最少的节点。
type leastLoadBalancer struct{}

// LeastLoad 创建最少负载均衡器实例。
func LeastLoad() Balancer {
	return leastLoadBalancer{}
}

// Pick 按 currentCalls 升序排列节点，选择负载最小的可用节点。
func (leastLoadBalancer) Pick(nodes []*model.Node, key string) (*model.Node, error) {
	candidates := availableNodes(nodes)
	if len(candidates) == 0 {
		return nil, ErrNoNode
	}
	// 按当前负载排序：负载低的优先，负载相同时按 ID 字典序稳定排序
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.CurrentCalls != right.CurrentCalls {
			return left.CurrentCalls < right.CurrentCalls
		}
		return left.ID < right.ID
	})
	return cloneNode(candidates[0]), nil
}

// consistentHashBalancer 一致性哈希负载均衡器：保证同一 key 始终路由到同一节点。
type consistentHashBalancer struct {
	replicas int // 每个节点的虚拟节点数
}

// ConsistentHash 创建一致性哈希负载均衡器。replicas 控制虚拟节点数（默认 64）。
func ConsistentHash(replicas int) Balancer {
	if replicas <= 0 {
		replicas = 64
	}
	return consistentHashBalancer{replicas: replicas}
}

// Pick 通过一致性哈希环选择节点：
// 1. 为每个可用节点创建 replicas 个虚拟节点并计算 CRC32 哈希
// 2. 对 key 计算哈希值
// 3. 在哈希环上顺时针查找最近的虚拟节点
func (b consistentHashBalancer) Pick(nodes []*model.Node, key string) (*model.Node, error) {
	candidates := availableNodes(nodes)
	if len(candidates) == 0 {
		return nil, ErrNoNode
	}
	// key 为空时使用默认值，保证总是能路由
	if strings.TrimSpace(key) == "" {
		key = "default"
	}
	// 构建哈希环：每个物理节点映射为 replicas 个虚拟节点
	ring := make([]hashPoint, 0, len(candidates)*b.replicas)
	for _, node := range candidates {
		for replica := 0; replica < b.replicas; replica++ {
			ring = append(ring, hashPoint{
				hash: crc32.ChecksumIEEE([]byte(node.ID + "#" + strconv.Itoa(replica))),
				node: node,
			})
		}
	}
	// 哈希环排序
	sort.Slice(ring, func(i, j int) bool {
		if ring[i].hash != ring[j].hash {
			return ring[i].hash < ring[j].hash
		}
		return ring[i].node.ID < ring[j].node.ID
	})
	// 二分查找 key 在环上的位置，顺时针取第一个节点
	target := crc32.ChecksumIEEE([]byte(key))
	index := sort.Search(len(ring), func(i int) bool {
		return ring[i].hash >= target
	})
	// 超出环范围时回绕到第一个节点
	if index == len(ring) {
		index = 0
	}
	return cloneNode(ring[index].node), nil
}

// hashPoint 哈希环上的一个虚拟节点：包含哈希值和对应的物理节点指针。
type hashPoint struct {
	hash uint32
	node *model.Node
}

// availableNodes 过滤并排序可用节点：排除 nil、空 ID、离线(status!=up)、满载的节点。
func availableNodes(nodes []*model.Node) []*model.Node {
	candidates := make([]*model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || node.ID == "" {
			continue
		}
		if node.Status != model.NodeStatusUp {
			continue
		}
		// 达到最大容量上限的节点不可用
		if node.MaxCalls > 0 && node.CurrentCalls >= node.MaxCalls {
			continue
		}
		candidates = append(candidates, node)
	}
	// 按 ID 稳定排序，保证同等条件下选择结果可重现
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

// cloneNode 深拷贝节点信息，避免调用方修改内部状态。
func cloneNode(node *model.Node) *model.Node {
	if node == nil {
		return nil
	}
	clone := *node
	// 深拷贝切片和 map，防止浅拷贝共享底层数据
	if node.Capabilities != nil {
		clone.Capabilities = append([]string(nil), node.Capabilities...)
	}
	if node.Labels != nil {
		clone.Labels = make(map[string]string, len(node.Labels))
		for key, value := range node.Labels {
			clone.Labels[key] = value
		}
	}
	return &clone
}
