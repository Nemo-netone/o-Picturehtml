//  etcd客户端工具：支持etcd/memory双模式
package etcdutil_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
)

func TestNewClient_DefaultsToMemory(t *testing.T) {
	client, err := etcdutil.NewClient("memory", []string{"http://127.0.0.1:2379"}, 0)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()
	if _, ok := client.(*etcdutil.MemoryClient); !ok {
		t.Fatalf("expected memory client, got %T", client)
	}
}

// 作用: 验证 Test Etcd Client_ Put Get Delete 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdClient_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	resp, err := client.Put(ctx, "/pbx/test/key", []byte("value"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if resp.Version != 1 {
		t.Fatalf("expected version 1, got %d", resp.Version)
	}

	item, err := client.Get(ctx, "/pbx/test/key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(item.Value) != "value" {
		t.Fatalf("unexpected value: %s", item.Value)
	}

	if err := client.Delete(ctx, "/pbx/test/key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := client.Get(ctx, "/pbx/test/key"); !errors.Is(err, etcdutil.ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

// 作用: 验证 Test Etcd Client_ Prefix Get 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdClient_PrefixGet(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	_, _ = client.Put(ctx, "/pbx/nodes/media/media-01", []byte("one"))
	_, _ = client.Put(ctx, "/pbx/nodes/media/media-02", []byte("two"))
	_, _ = client.Put(ctx, "/pbx/nodes/signaling/gw-01", []byte("gw"))

	items, err := client.GetPrefix(ctx, "/pbx/nodes/media/")
	if err != nil {
		t.Fatalf("get prefix: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two media nodes, got %d", len(items))
	}
}

// 作用: 验证 Test Etcd Lease_ Grant Keep Alive Revoke 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdLease_GrantKeepAliveRevoke(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	lease, err := client.Grant(ctx, time.Second)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	if _, err := client.Put(ctx, "/pbx/nodes/media/media-01", []byte("node"), etcdutil.WithLease(lease.ID)); err != nil {
		t.Fatalf("put with lease: %v", err)
	}

	keepalive, err := client.KeepAlive(ctx, lease.ID)
	if err != nil {
		t.Fatalf("keepalive: %v", err)
	}
	select {
	case ka := <-keepalive:
		if ka.LeaseID != lease.ID {
			t.Fatalf("unexpected keepalive lease id: %d", ka.LeaseID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for keepalive")
	}

	if err := client.Revoke(ctx, lease.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := client.Get(ctx, "/pbx/nodes/media/media-01"); !errors.Is(err, etcdutil.ErrKeyNotFound) {
		t.Fatalf("expected leased key to be deleted, got %v", err)
	}
}

// 作用: 验证 Test Etcd Txn_ Create If Not Exists 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdTxn_CreateIfNotExists(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	if err := client.CreateIfNotExists(ctx, "/pbx/calls/call-1/owner", []byte("owner")); err != nil {
		t.Fatalf("create if not exists: %v", err)
	}
	if err := client.CreateIfNotExists(ctx, "/pbx/calls/call-1/owner", []byte("owner-2")); !errors.Is(err, etcdutil.ErrKeyExists) {
		t.Fatalf("expected ErrKeyExists, got %v", err)
	}
}

// 作用: 验证 Test Etcd Txn_ Compare Version 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdTxn_CompareVersion(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	resp, err := client.Put(ctx, "/pbx/config/routes/t001/r1", []byte("v1"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := client.UpdateIfVersion(ctx, "/pbx/config/routes/t001/r1", resp.Version, []byte("v2")); err != nil {
		t.Fatalf("update if version: %v", err)
	}
	if err := client.UpdateIfVersion(ctx, "/pbx/config/routes/t001/r1", resp.Version, []byte("v3")); !errors.Is(err, etcdutil.ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch, got %v", err)
	}
}

// 作用: 验证 Test Etcd Watch_ Put Delete Events 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestEtcdWatch_PutDeleteEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := etcdutil.NewMemoryClient()

	watch := client.WatchPrefix(ctx, "/pbx/watch/")
	_, _ = client.Put(ctx, "/pbx/watch/key", []byte("value"))
	_ = client.Delete(ctx, "/pbx/watch/key")

	first := mustReadEvent(t, watch)
	if first.Type != etcdutil.EventPut {
		t.Fatalf("expected put event, got %s", first.Type)
	}
	second := mustReadEvent(t, watch)
	if second.Type != etcdutil.EventDelete {
		t.Fatalf("expected delete event, got %s", second.Type)
	}
}

// 作用: 验证 Test Etcd Watch_ Resume After Cancel 场景的行为。
func TestEtcdWatch_ResumeAfterCancel(t *testing.T) {
	ctx := context.Background()
	client := etcdutil.NewMemoryClient()

	resp, _ := client.Put(ctx, "/pbx/resume/key1", []byte("v1"))
	_, _ = client.Put(ctx, "/pbx/resume/key2", []byte("v2"))

	events := client.ResumeWatchPrefix(ctx, "/pbx/resume/", resp.Revision)
	event := mustReadEvent(t, events)
	if event.Key != "/pbx/resume/key2" {
		t.Fatalf("expected key2 after revision %d, got %s", resp.Revision, event.Key)
	}
}

// 作用: 读取测试所需的 must Read Event，失败时终止测试。
func mustReadEvent(t *testing.T, ch <-chan etcdutil.Event) etcdutil.Event {
	t.Helper()

	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
	return etcdutil.Event{}
}

