// etcd客户端实现：支持真实etcd和内存模式
//
// 本文件是 SimulSpeak 的 etcd 客户端封装，提供两种运行模式：
//   - etcd 模式：连接真实 etcd 集群，用于生产环境（节点注册、配置中心、会话存储）
//   - 内存模式：纯内存 KV 存储，用于本地开发和单元测试
//
// 封装了 Put/Get/Delete/Watch/Lease 等基础操作，并提供了 CreateIfNotExists（CAS）、
// UpdateIfVersion（乐观锁）等高级操作，满足注册中心和会话管理的并发安全需求。
package etcdutil

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// ModeEtcd 表示 etcd 连接模式为真实 etcd 集群。
const ModeEtcd = "etcd"

// NewClient 创建 registry/config/session 使用的 KV client。
// mode 为 "etcd" 时连接真实 etcd 集群；其它值使用内存实现，便于本地开发和单测。
func NewClient(mode string, endpoints []string, dialTimeout time.Duration) (Client, error) {
	if !strings.EqualFold(strings.TrimSpace(mode), ModeEtcd) {
		return NewMemoryClient(), nil
	}
	if len(endpoints) == 0 {
		return nil, errors.New("etcd endpoints are required")
	}
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("dial etcd: %w", err)
	}
	return &EtcdClient{client: client}, nil
}

// EtcdClient 是真实 etcd 集群的 KV 客户端封装。
type EtcdClient struct {
	client *clientv3.Client
}

// Put 写入键值对到 etcd，支持租约绑定选项。返回包含版本号的 PutResponse。
func (c *EtcdClient) Put(ctx context.Context, key string, value []byte, opts ...PutOption) (*PutResponse, error) {
	options := collectPutOptions(opts...)
	putOpts := make([]clientv3.OpOption, 0, 1)
	if options.leaseID != 0 {
		putOpts = append(putOpts, clientv3.WithLease(clientv3.LeaseID(options.leaseID)))
	}
	resp, err := c.client.Put(ctx, key, string(value), putOpts...)
	if err != nil {
		return nil, err
	}
	// 回读获取版本号和 revision（etcd Put 返回值不包含所有字段）
	item, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &PutResponse{Key: key, Version: item.Version, Revision: resp.Header.Revision}, nil
}

// Get 读取单个 key 的当前值及元数据。key 不存在时返回 ErrKeyNotFound。
func (c *EtcdClient) Get(ctx context.Context, key string) (Item, error) {
	resp, err := c.client.Get(ctx, key)
	if err != nil {
		return Item{}, err
	}
	if len(resp.Kvs) == 0 {
		return Item{}, ErrKeyNotFound
	}
	return itemFromKV(resp.Kvs[0], resp.Header.Revision), nil
}

// GetPrefix 读取指定前缀下的所有键值对，结果按 key 字典序排序。
func (c *EtcdClient) GetPrefix(ctx context.Context, prefix string) ([]Item, error) {
	resp, err := c.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		items = append(items, itemFromKV(kv, resp.Header.Revision))
	}
	// 按 key 排序保证输出稳定
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})
	return items, nil
}

// Delete 删除指定 key。key 不存在时返回 ErrKeyNotFound。
func (c *EtcdClient) Delete(ctx context.Context, key string) error {
	resp, err := c.client.Delete(ctx, key)
	if err != nil {
		return err
	}
	if resp.Deleted == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// Grant 创建带 TTL 的租约，用于节点注册的自动过期机制。
func (c *EtcdClient) Grant(ctx context.Context, ttl time.Duration) (Lease, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	ttlSeconds := int64((ttl + time.Second - 1) / time.Second)
	resp, err := c.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return Lease{}, err
	}
	return Lease{ID: LeaseID(resp.ID), TTL: time.Duration(resp.TTL) * time.Second}, nil
}

// KeepAlive 启动租约续约：返回一个 channel，持续接收心跳成功事件。
// 调用方应持续消费该 channel（或使用 drainKeepAlive），否则续约会被阻塞。
func (c *EtcdClient) KeepAlive(ctx context.Context, id LeaseID) (<-chan LeaseKeepAliveResponse, error) {
	raw, err := c.client.KeepAlive(ctx, clientv3.LeaseID(id))
	if err != nil {
		return nil, err
	}
	out := make(chan LeaseKeepAliveResponse, 1)
	go func() {
		defer close(out)
		for resp := range raw {
			if resp == nil {
				continue
			}
			event := LeaseKeepAliveResponse{
				LeaseID: LeaseID(resp.ID),
				TTL:     time.Duration(resp.TTL) * time.Second,
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Revoke 撤销租约，与之绑定的所有 key 立即过期并触发 Delete 事件。
func (c *EtcdClient) Revoke(ctx context.Context, id LeaseID) error {
	_, err := c.client.Revoke(ctx, clientv3.LeaseID(id))
	return err
}

// CreateIfNotExists 原子创建：仅当 key 不存在时才写入，否则返回 ErrKeyExists。
// 使用 etcd 事务（Txn + CreateRevision 比较）实现 CAS 语义。
func (c *EtcdClient) CreateIfNotExists(ctx context.Context, key string, value []byte, opts ...PutOption) error {
	options := collectPutOptions(opts...)
	putOpts := make([]clientv3.OpOption, 0, 1)
	if options.leaseID != 0 {
		putOpts = append(putOpts, clientv3.WithLease(clientv3.LeaseID(options.leaseID)))
	}
	// 事务：比较 CreateRevision == 0（即 key 不存在），成功时写入
	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(value), putOpts...)).
		Commit()
	if err != nil {
		return err
	}
	if !resp.Succeeded {
		return ErrKeyExists
	}
	return nil
}

// UpdateIfVersion 乐观锁更新：仅当 key 的当前版本号与 version 匹配时才写入。
// 版本不匹配时返回 ErrVersionMismatch，key 不存在时返回 ErrKeyNotFound。
// 用于防止并发写入冲突（如会话状态更新时的 epoch 校验）。
func (c *EtcdClient) UpdateIfVersion(ctx context.Context, key string, version int64, value []byte, opts ...PutOption) error {
	options := collectPutOptions(opts...)
	putOpts := make([]clientv3.OpOption, 0, 1)
	if options.leaseID != 0 {
		putOpts = append(putOpts, clientv3.WithLease(clientv3.LeaseID(options.leaseID)))
	}
	// 事务：比较 Version == 期望值，成功时写入
	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", version)).
		Then(clientv3.OpPut(key, string(value), putOpts...)).
		Commit()
	if err != nil {
		return err
	}
	if resp.Succeeded {
		return nil
	}
	// 版本不匹配：区分"key 不存在"和"版本冲突"
	if _, err := c.Get(ctx, key); errors.Is(err, ErrKeyNotFound) {
		return ErrKeyNotFound
	}
	return ErrVersionMismatch
}

// WatchPrefix 监听指定前缀下的所有变更事件（PUT/DELETE），返回事件 channel。
func (c *EtcdClient) WatchPrefix(ctx context.Context, prefix string) <-chan Event {
	return c.ResumeWatchPrefix(ctx, prefix, 0)
}

// ResumeWatchPrefix 从指定 revision 之后开始监听前缀变更，支持断线重连后继续追踪。
func (c *EtcdClient) ResumeWatchPrefix(ctx context.Context, prefix string, afterRevision int64) <-chan Event {
	out := make(chan Event, 32)
	opts := []clientv3.OpOption{clientv3.WithPrefix(), clientv3.WithPrevKV()}
	if afterRevision > 0 {
		opts = append(opts, clientv3.WithRev(afterRevision+1))
	}
	raw := c.client.Watch(ctx, prefix, opts...)
	// 后台协程将 etcd 事件转换为内部 Event 类型
	go func() {
		defer close(out)
		for resp := range raw {
			for _, event := range resp.Events {
				mapped := eventFromEtcd(event, resp.Header.Revision)
				select {
				case out <- mapped:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// Close 关闭 etcd 客户端连接。
func (c *EtcdClient) Close() error {
	return c.client.Close()
}

// collectPutOptions 聚合 PutOption 列表到 putOptions 结构。
func collectPutOptions(opts ...PutOption) putOptions {
	options := putOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

// itemFromKV 将 etcd KeyValue 转换为内部 Item 类型。
func itemFromKV(kv *mvccpb.KeyValue, revision int64) Item {
	return Item{
		Key:      string(kv.Key),
		Value:    cloneBytes(kv.Value),
		Version:  kv.Version,
		Revision: revision,
		LeaseID:  LeaseID(kv.Lease),
	}
}

// eventFromEtcd 将 etcd 事件转换为内部 Event 类型（区分 PUT/DELETE）。
func eventFromEtcd(event *clientv3.Event, revision int64) Event {
	out := Event{
		Key:       string(event.Kv.Key),
		Value:     cloneBytes(event.Kv.Value),
		Version:   event.Kv.Version,
		Revision:  revision,
		Timestamp: time.Now().UTC(),
	}
	if event.PrevKv != nil {
		out.PrevValue = cloneBytes(event.PrevKv.Value)
	}
	switch event.Type {
	case clientv3.EventTypeDelete:
		out.Type = EventDelete
	default:
		out.Type = EventPut
	}
	return out
}
