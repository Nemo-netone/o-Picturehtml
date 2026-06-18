//  PBX节点客户端库(SDK)：节点池+负载均衡+WebSocket+Provider配置+消息中继
package client

import (
	"errors"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

func TestBalancerLeastLoadFiltersUnavailableNodes(t *testing.T) {
	nodes := []*model.Node{
		testNode("media-full", 10, 10, model.NodeStatusUp),
		testNode("media-draining", 0, 10, model.NodeStatusDraining),
		testNode("media-b", 3, 10, model.NodeStatusUp),
		testNode("media-a", 1, 10, model.NodeStatusUp),
	}
	got, err := LeastLoad().Pick(nodes, "call-1")
	if err != nil {
		t.Fatalf("pick least load: %v", err)
	}
	if got.ID != "media-a" {
		t.Fatalf("expected media-a, got %#v", got)
	}
}

func TestBalancerRoundRobinRotatesAvailableNodes(t *testing.T) {
	balancer := RoundRobin()
	nodes := []*model.Node{
		testNode("media-a", 0, 10, model.NodeStatusUp),
		testNode("media-b", 0, 10, model.NodeStatusUp),
	}
	first, err := balancer.Pick(nodes, "")
	if err != nil {
		t.Fatalf("first pick: %v", err)
	}
	second, err := balancer.Pick(nodes, "")
	if err != nil {
		t.Fatalf("second pick: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("expected round robin to rotate, got %s then %s", first.ID, second.ID)
	}
}

func TestBalancerConsistentHashIsStable(t *testing.T) {
	balancer := ConsistentHash(8)
	nodes := []*model.Node{
		testNode("media-a", 0, 10, model.NodeStatusUp),
		testNode("media-b", 0, 10, model.NodeStatusUp),
		testNode("media-c", 0, 10, model.NodeStatusUp),
	}
	first, err := balancer.Pick(nodes, "tenant-a-call-1")
	if err != nil {
		t.Fatalf("first pick: %v", err)
	}
	for i := 0; i < 10; i++ {
		got, err := balancer.Pick(nodes, "tenant-a-call-1")
		if err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
		if got.ID != first.ID {
			t.Fatalf("consistent hash moved from %s to %s", first.ID, got.ID)
		}
	}
}

func TestBalancerNoNode(t *testing.T) {
	_, err := LeastLoad().Pick([]*model.Node{testNode("media-full", 1, 1, model.NodeStatusUp)}, "")
	if !errors.Is(err, ErrNoNode) {
		t.Fatalf("expected ErrNoNode, got %v", err)
	}
}

func testNode(id string, currentCalls, maxCalls int, status model.NodeStatus) *model.Node {
	return &model.Node{
		ID:           id,
		Type:         model.NodeTypeMedia,
		Endpoint:     "ws://127.0.0.1:8081/pbx/ws",
		Status:       status,
		CurrentCalls: currentCalls,
		MaxCalls:     maxCalls,
	}
}

