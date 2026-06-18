//  服务注册中心：节点注册+注销+发现+负载上报+Watch
package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	pbxerrors "github.com/SATA260/SimulSpeak1/internal/errors"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

const (
	nodeRoot       = "/pbx/nodes"
	capabilityRoot = "/pbx/capabilities"
)

type NodeEventType string

const (
	NodeEventUp   NodeEventType = "up"
	NodeEventDown NodeEventType = "down"
)

type NodeEvent struct {
	Type NodeEventType
	Node *model.Node
	Key  string
}

type Options struct {
	LeaseTTL time.Duration
}

type Registry struct {
	client  etcdutil.Client
	options Options
	mu      sync.Mutex
	leases  map[string]etcdutil.LeaseID
}

// New 创建节点注册中心，默认租约 TTL 30s。
func New(client etcdutil.Client, options Options) *Registry {
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 30 * time.Second
	}
	return &Registry{
		client:  client,
		options: options,
		leases:  map[string]etcdutil.LeaseID{},
	}
}

// Register 注册一个节点到 etcd 并启动租约 keepalive（节点下线后记录自动过期）。
func (r *Registry) Register(ctx context.Context, node *model.Node) error {
	if node == nil {
		return errors.New("node is required")
	}
	if node.ID == "" || node.Type == "" {
		return errors.New("node id and type are required")
	}

	lease, err := r.client.Grant(ctx, r.options.LeaseTTL)
	if err != nil {
		return fmt.Errorf("grant node lease: %w", err)
	}
	node.LeaseID = int64(lease.ID)

	data, err := etcdutil.Marshal(node)
	if err != nil {
		return err
	}
	key := NodeKey(node.Type, node.ID)
	if _, err := r.client.Put(ctx, key, data, etcdutil.WithLease(lease.ID)); err != nil {
		return fmt.Errorf("put node: %w", err)
	}

	r.mu.Lock()
	r.leases[key] = lease.ID
	r.mu.Unlock()

	keepalive, err := r.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("keepalive node lease: %w", err)
	}
	go drainKeepAlive(keepalive)

	return nil
}

// Deregister 撤销节点注册（有租约时撤销租约，否则直接删除 key）。
func (r *Registry) Deregister(ctx context.Context, nodeType model.NodeType, nodeID string) error {
	key := NodeKey(nodeType, nodeID)
	r.mu.Lock()
	leaseID, ok := r.leases[key]
	delete(r.leases, key)
	r.mu.Unlock()

	if ok {
		if err := r.client.Revoke(ctx, leaseID); err != nil && !errors.Is(err, etcdutil.ErrLeaseNotFound) {
			return fmt.Errorf("revoke node lease: %w", err)
		}
		return nil
	}

	if err := r.client.Delete(ctx, key); err != nil && !errors.Is(err, etcdutil.ErrKeyNotFound) {
		return fmt.Errorf("delete node: %w", err)
	}
	return nil
}

// GetNode 从 etcd 读取单个节点的注册信息。
func (r *Registry) GetNode(ctx context.Context, nodeType model.NodeType, nodeID string) (*model.Node, error) {
	item, err := r.client.Get(ctx, NodeKey(nodeType, nodeID))
	if errors.Is(err, etcdutil.ErrKeyNotFound) {
		return nil, pbxerrors.ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}

	var node model.Node
	if err := etcdutil.Unmarshal(item.Value, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// ListNodes 列出某类型的所有已注册节点，按 ID 排序。
func (r *Registry) ListNodes(ctx context.Context, nodeType model.NodeType) ([]*model.Node, error) {
	items, err := r.client.GetPrefix(ctx, NodePrefix(nodeType))
	if err != nil {
		return nil, err
	}

	nodes := make([]*model.Node, 0, len(items))
	for _, item := range items {
		var node model.Node
		if err := etcdutil.Unmarshal(item.Value, &node); err != nil {
			return nil, err
		}
		nodes = append(nodes, &node)
	}
	// 按 ID 排序保证输出稳定。
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes, nil
}

// WatchNodes 监听节点上下线事件，返回一个 unbuffered channel 用于接收 NodeEvent（up/down）。
func (r *Registry) WatchNodes(ctx context.Context, nodeType model.NodeType) <-chan NodeEvent {
	events := r.client.WatchPrefix(ctx, NodePrefix(nodeType))
	out := make(chan NodeEvent, 16)

	// 后台协程将 etcd 的 put/delete 事件映射为 up/down NodeEvent。
	go func() {
		defer close(out)
		for event := range events {
			nodeEvent := NodeEvent{Key: event.Key}
			switch event.Type {
			case etcdutil.EventPut:
				var node model.Node
				if err := etcdutil.Unmarshal(event.Value, &node); err != nil {
					continue
				}
				nodeEvent.Type = NodeEventUp
				nodeEvent.Node = &node
			case etcdutil.EventDelete:
				nodeEvent.Type = NodeEventDown
			default:
				continue
			}

			select {
			case out <- nodeEvent:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

// UpdateLoad 乐观更新节点的当前通话数（用于负载上报）。
func (r *Registry) UpdateLoad(ctx context.Context, nodeType model.NodeType, nodeID string, currentCalls int) error {
	node, err := r.GetNode(ctx, nodeType, nodeID)
	if err != nil {
		return err
	}
	node.CurrentCalls = currentCalls

	item, err := r.client.Get(ctx, NodeKey(nodeType, nodeID))
	if err != nil {
		return err
	}
	data, err := etcdutil.Marshal(node)
	if err != nil {
		return err
	}
	if err := r.client.UpdateIfVersion(ctx, NodeKey(nodeType, nodeID), item.Version, data, etcdutil.WithLease(etcdutil.LeaseID(node.LeaseID))); err != nil {
		return fmt.Errorf("update node load: %w", err)
	}
	return nil
}

// RegisterCapability 向 etcd 注册一个 PBX 音频能力（VAD/ASR/TTS）。
func (r *Registry) RegisterCapability(ctx context.Context, capability model.Capability) error {
	if capability.ID == "" || capability.Type == "" {
		return errors.New("capability id and type are required")
	}
	data, err := etcdutil.Marshal(capability)
	if err != nil {
		return err
	}
	if _, err := r.client.Put(ctx, CapabilityKey(capability.Type, capability.ID), data); err != nil {
		return fmt.Errorf("put capability: %w", err)
	}
	return nil
}

// ListCapabilities 列出匹配选择器的 AI 能力，按 ID 排序。
func (r *Registry) ListCapabilities(ctx context.Context, selector model.CapabilitySelector) ([]model.Capability, error) {
	prefix := capabilityRoot + "/"
	if selector.Type != "" {
		prefix = CapabilityPrefix(selector.Type)
	}

	items, err := r.client.GetPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	capabilities := make([]model.Capability, 0, len(items))
	for _, item := range items {
		var capability model.Capability
		if err := etcdutil.Unmarshal(item.Value, &capability); err != nil {
			return nil, err
		}
		if capability.Matches(selector) {
			capabilities = append(capabilities, capability)
		}
	}
	// 按 ID 排序保证输出稳定。
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].ID < capabilities[j].ID
	})
	return capabilities, nil
}

// NodeKey 构建节点注册的 etcd key：/pbx/nodes/{type}/{id}。
func NodeKey(nodeType model.NodeType, nodeID string) string {
	return etcdutil.JoinKey(nodeRoot, string(nodeType), nodeID)
}

// NodePrefix 构建节点注册的 etcd 前缀：/pbx/nodes/{type}/。
func NodePrefix(nodeType model.NodeType) string {
	return etcdutil.JoinKey(nodeRoot, string(nodeType)) + "/"
}

// CapabilityKey 构建 AI 能力的 etcd key：/pbx/capabilities/{type}/{id}。
func CapabilityKey(capabilityType model.CapabilityType, id string) string {
	return etcdutil.JoinKey(capabilityRoot, string(capabilityType), id)
}

// CapabilityPrefix 构建 AI 能力的 etcd 前缀：/pbx/capabilities/{type}/。
func CapabilityPrefix(capabilityType model.CapabilityType) string {
	return etcdutil.JoinKey(capabilityRoot, string(capabilityType)) + "/"
}

// drainKeepAlive 消费 keepalive channel 以维持租约（防止满而阻塞）。
func drainKeepAlive(ch <-chan etcdutil.LeaseKeepAliveResponse) {
	for range ch {
	}
}

