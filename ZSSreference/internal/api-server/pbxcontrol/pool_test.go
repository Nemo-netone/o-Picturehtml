//  PBX控制连接池：管理api-server到多个pbx-node的WebSocket控制连接
package pbxcontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxprotocol "github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
)

func TestPoolBindRetriesAndRoutesByConnection(t *testing.T) {
	messages := make(chan pbxprotocol.Message, 8)
	picker := &fakePicker{nodes: []*model.Node{
		{ID: "bad", Type: model.NodeTypeMedia, Endpoint: "bad://pbx", Status: model.NodeStatusUp, MaxCalls: 10, CurrentCalls: 0},
		{ID: "good", Type: model.NodeTypeMedia, Endpoint: "ws://good/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 10, CurrentCalls: 1},
	}}
	pool := NewPool(picker, nil, string(sdk.PolicyLeastLoad))
	pool.dial = fakeDialer(messages)
	defer func() { _ = pool.Close() }()

	nodeID, err := pool.Bind(context.Background(), "conn-1", "call-1", "call-1")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if nodeID != "good" {
		t.Fatalf("expected retry to bind good node, got %s", nodeID)
	}
	if err := pool.Send(context.Background(), pbxprotocol.Message{Type: pbxprotocol.TypeWebRTCOffer, ConnectionID: "conn-1", CallID: "call-1", SDP: "offer"}); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	if err := pool.Send(context.Background(), pbxprotocol.Message{Type: pbxprotocol.TypeICE, ConnectionID: "conn-1", Candidate: "candidate"}); err != nil {
		t.Fatalf("send ice: %v", err)
	}

	offer := readCapturedPBXMessage(t, messages)
	ice := readCapturedPBXMessage(t, messages)
	if offer.Type != pbxprotocol.TypeWebRTCOffer || ice.Type != pbxprotocol.TypeICE {
		t.Fatalf("unexpected messages: %#v %#v", offer, ice)
	}
}

func TestPoolUnbindRemovesConnectionRoute(t *testing.T) {
	picker := &fakePicker{nodes: []*model.Node{
		{ID: "good", Type: model.NodeTypeMedia, Endpoint: "ws://good/pbx/ws", Status: model.NodeStatusUp, MaxCalls: 10},
	}}
	pool := NewPool(picker, nil, string(sdk.PolicyLeastLoad))
	pool.dial = fakeDialer(make(chan pbxprotocol.Message, 8))
	defer func() { _ = pool.Close() }()

	if _, err := pool.Bind(context.Background(), "conn-1", "call-1", "call-1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	pool.Unbind("conn-1")
	if err := pool.Send(context.Background(), pbxprotocol.Message{Type: pbxprotocol.TypeICE, ConnectionID: "conn-1"}); err == nil {
		t.Fatal("expected send after unbind to fail")
	}
}

func TestPoolBindReservesLeastLoadDuringBurst(t *testing.T) {
	nodes := make([]*model.Node, 0, 10)
	for i := 0; i < 10; i++ {
		id := "media-" + string(rune('a'+i))
		nodes = append(nodes, &model.Node{
			ID:       id,
			Type:     model.NodeTypeMedia,
			Endpoint: "ws://" + id + "/pbx/ws",
			Status:   model.NodeStatusUp,
			MaxCalls: 100,
		})
	}
	pool := NewPool(&fakePicker{nodes: nodes}, nil, string(sdk.PolicyLeastLoad))
	pool.dial = fakeDialer(make(chan pbxprotocol.Message, 128))
	defer func() { _ = pool.Close() }()

	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		connectionID := fmt.Sprintf("conn-%03d", i)
		callID := fmt.Sprintf("call-%03d", i)
		nodeID, err := pool.Bind(context.Background(), connectionID, callID, callID)
		if err != nil {
			t.Fatalf("bind %d: %v", i, err)
		}
		counts[nodeID]++
	}

	if len(counts) != len(nodes) {
		t.Fatalf("expected all nodes to receive reservations, got %#v", counts)
	}
	for _, node := range nodes {
		if counts[node.ID] != 10 {
			t.Fatalf("expected %s to receive 10 reservations, got counts=%#v", node.ID, counts)
		}
	}
}

type fakePicker struct {
	nodes []*model.Node
}

func (p *fakePicker) Pick(policy, key string) (*model.Node, error) {
	return sdk.BalancerFor(policy).Pick(p.Nodes(), key)
}

func (p *fakePicker) Nodes() []*model.Node {
	out := make([]*model.Node, 0, len(p.nodes))
	for _, node := range p.nodes {
		clone := *node
		out = append(out, &clone)
	}
	return out
}

type recordingControlClient struct {
	messages chan<- pbxprotocol.Message
}

func (c recordingControlClient) Send(ctx context.Context, message pbxprotocol.Message) error {
	select {
	case c.messages <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c recordingControlClient) Close() error {
	return nil
}

func fakeDialer(messages chan<- pbxprotocol.Message) dialFunc {
	return func(ctx context.Context, url string, handler pbxprotocol.Handler) (controlClient, error) {
		if strings.HasPrefix(url, "bad://") {
			return nil, errors.New("dial failed")
		}
		return recordingControlClient{messages: messages}, nil
	}
}

func readCapturedPBXMessage(t *testing.T, messages <-chan pbxprotocol.Message) pbxprotocol.Message {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pbx message")
		return pbxprotocol.Message{}
	}
}

