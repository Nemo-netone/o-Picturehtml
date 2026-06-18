//  etcd客户端接口抽象
package etcdutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrKeyNotFound     = errors.New("key not found")
	ErrKeyExists       = errors.New("key already exists")
	ErrVersionMismatch = errors.New("version mismatch")
	ErrLeaseNotFound   = errors.New("lease not found")
)

type EventType string

const (
	EventPut    EventType = "PUT"
	EventDelete EventType = "DELETE"
)

type PutOption func(*putOptions)

type putOptions struct {
	leaseID LeaseID
}

type LeaseID int64

type PutResponse struct {
	Key      string
	Version  int64
	Revision int64
}

type Item struct {
	Key      string
	Value    []byte
	Version  int64
	Revision int64
	LeaseID  LeaseID
}

type Lease struct {
	ID  LeaseID
	TTL time.Duration
}

type LeaseKeepAliveResponse struct {
	LeaseID LeaseID
	TTL     time.Duration
}

type Event struct {
	Type      EventType
	Key       string
	Value     []byte
	PrevValue []byte
	Version   int64
	Revision  int64
	Timestamp time.Time
}

type Client interface {
	Put(ctx context.Context, key string, value []byte, opts ...PutOption) (*PutResponse, error)
	Get(ctx context.Context, key string) (Item, error)
	GetPrefix(ctx context.Context, prefix string) ([]Item, error)
	Delete(ctx context.Context, key string) error
	Grant(ctx context.Context, ttl time.Duration) (Lease, error)
	KeepAlive(ctx context.Context, id LeaseID) (<-chan LeaseKeepAliveResponse, error)
	Revoke(ctx context.Context, id LeaseID) error
	CreateIfNotExists(ctx context.Context, key string, value []byte, opts ...PutOption) error
	UpdateIfVersion(ctx context.Context, key string, version int64, value []byte, opts ...PutOption) error
	WatchPrefix(ctx context.Context, prefix string) <-chan Event
	ResumeWatchPrefix(ctx context.Context, prefix string, afterRevision int64) <-chan Event
	Close() error
}

// WithLease 返回一个 PutOption 将写入操作关联到指定租约（租约过期后 key 自动删除）。
func WithLease(id LeaseID) PutOption {
	return func(opts *putOptions) {
		opts.leaseID = id
	}
}

// Marshal 将值 JSON 序列化（封装 JSON 编码，统一错误信息）。
func Marshal(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal etcd value: %w", err)
	}
	return data, nil
}

// Unmarshal 将 JSON 数据反序列化到 target（封装 JSON 解码，统一错误信息）。
func Unmarshal(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal etcd value: %w", err)
	}
	return nil
}

// JoinKey 用 / 拼接路径段，自动去重前后斜杠，生成整洁的 etcd key。
func JoinKey(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return "/" + strings.Join(cleaned, "/")
}

type MemoryClient struct {
	mu            sync.RWMutex
	items         map[string]Item
	leases        map[LeaseID]*memoryLease
	nextLeaseID   LeaseID
	revision      int64
	watchers      map[int]watcher
	nextWatcherID int
	history       []Event
	closed        bool
}

type memoryLease struct {
	lease     Lease
	keys      map[string]struct{}
	expiresAt time.Time
}

type watcher struct {
	prefix string
	ch     chan Event
}

// NewMemoryClient 创建一个完全在内存中运行的 etcd 模拟实现。
func NewMemoryClient() *MemoryClient {
	return &MemoryClient{
		items:    map[string]Item{},
		leases:   map[LeaseID]*memoryLease{},
		watchers: map[int]watcher{},
		history:  make([]Event, 0, 128),
	}
}

// Put 写入 key-value，支持关联租约。key 存在时版本号递增，不存在时从 1 开始。
func (c *MemoryClient) Put(ctx context.Context, key string, value []byte, opts ...PutOption) (*PutResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	options := putOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if options.leaseID != 0 {
		lease, ok := c.leases[options.leaseID]
		if !ok {
			return nil, ErrLeaseNotFound
		}
		lease.keys[key] = struct{}{}
	}

	prev, existed := c.items[key]
	version := int64(1)
	if existed {
		version = prev.Version + 1
	}
	c.revision++
	item := Item{
		Key:      key,
		Value:    cloneBytes(value),
		Version:  version,
		Revision: c.revision,
		LeaseID:  options.leaseID,
	}
	c.items[key] = item
	c.publishLocked(Event{
		Type:      EventPut,
		Key:       key,
		Value:     cloneBytes(value),
		PrevValue: cloneBytes(prev.Value),
		Version:   version,
		Revision:  c.revision,
		Timestamp: time.Now().UTC(),
	})

	return &PutResponse{Key: key, Version: version, Revision: c.revision}, nil
}

// Get 读取单个 key，不存在返回 ErrKeyNotFound。
func (c *MemoryClient) Get(ctx context.Context, key string) (Item, error) {
	if err := ctx.Err(); err != nil {
		return Item{}, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	item, ok := c.items[key]
	if !ok {
		return Item{}, ErrKeyNotFound
	}
	item.Value = cloneBytes(item.Value)
	return item, nil
}

// GetPrefix 返回所有以 prefix 开头的 key-value，按 key 排序。
func (c *MemoryClient) GetPrefix(ctx context.Context, prefix string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make([]Item, 0)
	for key, item := range c.items {
		if strings.HasPrefix(key, prefix) {
			item.Value = cloneBytes(item.Value)
			items = append(items, item)
		}
	}
	// 按 key 排序保证输出稳定。
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})
	return items, nil
}

// Delete 删除 key，不存在时返回 ErrKeyNotFound（通过 deleteLocked）。
func (c *MemoryClient) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.deleteLocked(key)
}

// Grant 创建一个带 TTL 的租约，到期自动删除关联的 key。
func (c *MemoryClient) Grant(ctx context.Context, ttl time.Duration) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextLeaseID++
	lease := Lease{ID: c.nextLeaseID, TTL: ttl}
	c.leases[lease.ID] = &memoryLease{lease: lease, keys: map[string]struct{}{}, expiresAt: time.Now().Add(ttl)}
	go c.expireLease(lease.ID)
	return lease, nil
}

// KeepAlive 启动租约续期：返回一个 channel 定期收到续期响应，ctx 取消时停止。
func (c *MemoryClient) KeepAlive(ctx context.Context, id LeaseID) (<-chan LeaseKeepAliveResponse, error) {
	c.mu.RLock()
	lease, ok := c.leases[id]
	c.mu.RUnlock()
	if !ok {
		return nil, ErrLeaseNotFound
	}

	ch := make(chan LeaseKeepAliveResponse, 1)
	ch <- LeaseKeepAliveResponse{LeaseID: id, TTL: lease.lease.TTL}

	// 后台协程：每 TTL/3 刷新一次过期时间，ctx 取消时退出。
	go func() {
		ticker := time.NewTicker(maxDuration(lease.lease.TTL/3, 10*time.Millisecond))
		defer ticker.Stop()
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.Lock()
				if lease, ok := c.leases[id]; ok {
					lease.expiresAt = time.Now().Add(lease.lease.TTL)
				}
				c.mu.Unlock()
				select {
				case ch <- LeaseKeepAliveResponse{LeaseID: id, TTL: lease.lease.TTL}:
				default:
				}
			}
		}
	}()

	return ch, nil
}

// Revoke 撤销租约并删除其关联的所有 key。
func (c *MemoryClient) Revoke(ctx context.Context, id LeaseID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lease, ok := c.leases[id]
	if !ok {
		return ErrLeaseNotFound
	}
	for key := range lease.keys {
		_ = c.deleteLocked(key)
	}
	delete(c.leases, id)
	return nil
}

// CreateIfNotExists 仅当 key 不存在时写入，否则返回 ErrKeyExists（原子操作）。
func (c *MemoryClient) CreateIfNotExists(ctx context.Context, key string, value []byte, opts ...PutOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	_, exists := c.items[key]
	c.mu.RUnlock()
	if exists {
		return ErrKeyExists
	}

	_, err := c.Put(ctx, key, value, opts...)
	return err
}

// UpdateIfVersion 仅当 key 当前版本匹配时才写入，否则返回 ErrVersionMismatch（乐观并发控制）。
func (c *MemoryClient) UpdateIfVersion(ctx context.Context, key string, version int64, value []byte, opts ...PutOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	item, exists := c.items[key]
	c.mu.RUnlock()
	if !exists {
		return ErrKeyNotFound
	}
	if item.Version != version {
		return ErrVersionMismatch
	}

	_, err := c.Put(ctx, key, value, opts...)
	return err
}

// WatchPrefix 从当前 revision 开始监听 key 前缀变更事件。
func (c *MemoryClient) WatchPrefix(ctx context.Context, prefix string) <-chan Event {
	return c.ResumeWatchPrefix(ctx, prefix, c.currentRevision())
}

// ResumeWatchPrefix 从指定 revision 恢复监听：先补发历史事件，再推送实时变更。
func (c *MemoryClient) ResumeWatchPrefix(ctx context.Context, prefix string, afterRevision int64) <-chan Event {
	ch := make(chan Event, 32)

	c.mu.Lock()
	id := c.nextWatcherID
	c.nextWatcherID++
	c.watchers[id] = watcher{prefix: prefix, ch: ch}
	history := make([]Event, 0)
	for _, event := range c.history {
		if event.Revision > afterRevision && strings.HasPrefix(event.Key, prefix) {
			history = append(history, cloneEvent(event))
		}
	}
	c.mu.Unlock()

	// 后台协程：先补发 afterRevision 之后的历史事件，然后等 ctx 结束清理 watcher。
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.watchers, id)
			c.mu.Unlock()
			close(ch)
		}()
		for _, event := range history {
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
	}()

	return ch
}

// Close 关闭所有 watcher 并标记客户端为已关闭。
func (c *MemoryClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	for id, watcher := range c.watchers {
		close(watcher.ch)
		delete(c.watchers, id)
	}
	return nil
}

// currentRevision 返回当前全局 revision（线程安全读）。
func (c *MemoryClient) currentRevision() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision
}

// deleteLocked 在持有写锁时删除 key 并发布 DELETE 事件。
func (c *MemoryClient) deleteLocked(key string) error {
	prev, ok := c.items[key]
	if !ok {
		return ErrKeyNotFound
	}
	delete(c.items, key)
	if prev.LeaseID != 0 {
		if lease, ok := c.leases[prev.LeaseID]; ok {
			delete(lease.keys, key)
		}
	}
	c.revision++
	c.publishLocked(Event{
		Type:      EventDelete,
		Key:       key,
		PrevValue: cloneBytes(prev.Value),
		Version:   prev.Version,
		Revision:  c.revision,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

// expireLease 在后台每 10ms 检查一次租约是否过期，过期则删除关联 key。
func (c *MemoryClient) expireLease(id LeaseID) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		lease, ok := c.leases[id]
		if !ok {
			c.mu.Unlock()
			return
		}
		if time.Now().Before(lease.expiresAt) {
			c.mu.Unlock()
			continue
		}
		for key := range lease.keys {
			_ = c.deleteLocked(key)
		}
		delete(c.leases, id)
		c.mu.Unlock()
		return
	}
}

// publishLocked 将事件推送到所有匹配前缀的 watcher（持锁调用）。
func (c *MemoryClient) publishLocked(event Event) {
	event = cloneEvent(event)
	c.history = append(c.history, event)
	for _, watcher := range c.watchers {
		if strings.HasPrefix(event.Key, watcher.prefix) {
			select {
			case watcher.ch <- cloneEvent(event):
			default:
			}
		}
	}
}

// cloneEvent 深拷贝事件（避免 watcher 间数据竞争）。
func cloneEvent(event Event) Event {
	event.Value = cloneBytes(event.Value)
	event.PrevValue = cloneBytes(event.PrevValue)
	return event
}

// cloneBytes 深拷贝字节切片（防止并发修改）。
func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out
}

// maxDuration 返回两者中较大的 duration（Go 1.25 不再需要此函数，但保留用于兼容）。
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

