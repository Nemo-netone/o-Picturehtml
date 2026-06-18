//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

func TestNodePoolPickAndWatchUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := registry.New(etcdutil.NewMemoryClient(), registry.Options{})
	pool := NewNodePool(reg, model.NodeTypeMedia)

	if err := reg.Register(ctx, testNode("media-a", 2, 10, model.NodeStatusUp)); err != nil {
		t.Fatalf("register media-a: %v", err)
	}
	if err := reg.Register(ctx, testNode("media-b", 0, 10, model.NodeStatusUp)); err != nil {
		t.Fatalf("register media-b: %v", err)
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("start pool: %v", err)
	}

	got, err := pool.Pick(string(PolicyLeastLoad), "call-1")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got.ID != "media-b" {
		t.Fatalf("expected media-b, got %#v", got)
	}

	if err := reg.Deregister(ctx, model.NodeTypeMedia, "media-b"); err != nil {
		t.Fatalf("deregister media-b: %v", err)
	}
	waitForPoolNodeCount(t, pool, 1)
	got, err = pool.Pick(string(PolicyLeastLoad), "call-2")
	if err != nil {
		t.Fatalf("pick after delete: %v", err)
	}
	if got.ID != "media-a" {
		t.Fatalf("expected media-a after media-b removal, got %#v", got)
	}
}

func waitForPoolNodeCount(t *testing.T, pool *NodePool, count int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(pool.Nodes()) == count {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for pool node count %d, got %d", count, len(pool.Nodes()))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

