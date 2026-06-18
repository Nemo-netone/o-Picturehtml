//  PBX控制连接池：管理api-server到多个pbx-node的WebSocket控制连接
package pbxcontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxprotocol "github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
)

type Picker interface {
	Pick(policy, key string) (*model.Node, error)
	Nodes() []*model.Node
}

type controlClient interface {
	Send(context.Context, pbxprotocol.Message) error
	Close() error
}

type dialFunc func(ctx context.Context, url string, handler pbxprotocol.Handler) (controlClient, error)

type Pool struct {
	picker        Picker
	handler       pbxprotocol.Handler
	defaultPolicy string
	dial          dialFunc

	mu              sync.Mutex
	clients         map[string]controlClient
	bindings        map[string]string
	callBindings    map[string]string
	connectionCalls map[string]string
	reservations    map[string]int
}

func NewPool(picker Picker, handler pbxprotocol.Handler, defaultPolicy string) *Pool {
	if strings.TrimSpace(defaultPolicy) == "" {
		defaultPolicy = string(sdk.PolicyLeastLoad)
	}
	return &Pool{
		picker:          picker,
		handler:         handler,
		defaultPolicy:   defaultPolicy,
		dial:            dialPBXControl,
		clients:         map[string]controlClient{},
		bindings:        map[string]string{},
		callBindings:    map[string]string{},
		connectionCalls: map[string]string{},
		reservations:    map[string]int{},
	}
}

func (p *Pool) Bind(ctx context.Context, connectionID, callID, key string) (string, error) {
	if strings.TrimSpace(connectionID) == "" {
		return "", errors.New("connection id is required")
	}
	if strings.TrimSpace(key) == "" {
		key = firstNonEmpty(callID, connectionID)
	}
	if existing, ok := p.NodeID(connectionID); ok {
		return existing, nil
	}
	if p.picker == nil {
		return "", errors.New("pbx node picker is not configured")
	}

	tried := map[string]struct{}{}
	var lastErr error
	for {
		node, err := p.reserveNode(p.defaultPolicy, key, tried)
		if err != nil {
			if lastErr != nil {
				return "", lastErr
			}
			return "", err
		}
		tried[node.ID] = struct{}{}
		if _, err := p.clientForNode(ctx, node); err != nil {
			p.releaseReservation(node.ID)
			lastErr = err
			slog.WarnContext(ctx, "PBX control 节点连接失败，尝试其它节点",
				slog.String("nodeID", node.ID),
				slog.String("endpoint", node.Endpoint),
				slog.Any("error", err),
			)
			continue
		}

		p.mu.Lock()
		if existing := p.bindings[connectionID]; existing != "" {
			p.releaseReservationLocked(node.ID)
			p.mu.Unlock()
			return existing, nil
		}
		p.bindings[connectionID] = node.ID
		if strings.TrimSpace(callID) != "" {
			p.callBindings[callID] = node.ID
			p.connectionCalls[connectionID] = callID
		}
		p.mu.Unlock()
		return node.ID, nil
	}
}

func (p *Pool) Send(ctx context.Context, message pbxprotocol.Message) error {
	nodeID, ok := p.routeNodeID(message)
	if !ok {
		return errors.New("pbx node binding not found")
	}
	client, err := p.boundClient(ctx, nodeID)
	if err != nil {
		return err
	}
	if err := client.Send(ctx, message); err != nil {
		p.dropClient(nodeID)
		return err
	}
	if message.Type == pbxprotocol.TypeCloseSession {
		p.Unbind(message.ConnectionID)
	}
	return nil
}

func (p *Pool) NodeID(connectionID string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nodeID, ok := p.bindings[connectionID]
	return nodeID, ok
}

func (p *Pool) Unbind(connectionID string) {
	if strings.TrimSpace(connectionID) == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.bindings[connectionID]; !ok {
		return
	}
	nodeID := p.bindings[connectionID]
	delete(p.bindings, connectionID)
	if callID := p.connectionCalls[connectionID]; callID != "" {
		delete(p.callBindings, callID)
	}
	delete(p.connectionCalls, connectionID)
	p.releaseReservationLocked(nodeID)
}

func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var closeErr error
	for nodeID, client := range p.clients {
		if err := client.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close pbx control %s: %w", nodeID, err))
		}
		delete(p.clients, nodeID)
	}
	p.bindings = map[string]string{}
	p.callBindings = map[string]string{}
	p.connectionCalls = map[string]string{}
	p.reservations = map[string]int{}
	return closeErr
}

func (p *Pool) routeNodeID(message pbxprotocol.Message) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(message.ConnectionID) != "" {
		nodeID, ok := p.bindings[message.ConnectionID]
		if ok {
			return nodeID, true
		}
	}
	if strings.TrimSpace(message.CallID) != "" {
		nodeID, ok := p.callBindings[message.CallID]
		if ok {
			return nodeID, true
		}
	}
	return "", false
}

func (p *Pool) boundClient(ctx context.Context, nodeID string) (controlClient, error) {
	p.mu.Lock()
	client := p.clients[nodeID]
	p.mu.Unlock()
	if client != nil {
		return client, nil
	}
	if p.picker == nil {
		return nil, errors.New("pbx node picker is not configured")
	}
	for _, node := range p.picker.Nodes() {
		if node.ID == nodeID {
			return p.clientForNode(ctx, node)
		}
	}
	return nil, fmt.Errorf("pbx node %s not found", nodeID)
}

func (p *Pool) clientForNode(ctx context.Context, node *model.Node) (controlClient, error) {
	if node == nil {
		return nil, sdk.ErrNoNode
	}
	p.mu.Lock()
	client := p.clients[node.ID]
	p.mu.Unlock()
	if client != nil {
		return client, nil
	}
	if strings.TrimSpace(node.Endpoint) == "" {
		return nil, fmt.Errorf("pbx node %s endpoint is empty", node.ID)
	}
	connected, err := p.dial(ctx, node.Endpoint, p.handler)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if existing := p.clients[node.ID]; existing != nil {
		p.mu.Unlock()
		_ = connected.Close()
		return existing, nil
	}
	p.clients[node.ID] = connected
	p.mu.Unlock()
	slog.InfoContext(ctx, "PBX control WebSocket 已连接",
		slog.String("nodeID", node.ID),
		slog.String("endpoint", node.Endpoint),
	)
	return connected, nil
}

func (p *Pool) dropClient(nodeID string) {
	p.mu.Lock()
	client := p.clients[nodeID]
	delete(p.clients, nodeID)
	p.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func (p *Pool) reserveNode(policy, key string, tried map[string]struct{}) (*model.Node, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nodes := p.picker.Nodes()
	filtered := make([]*model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if _, ok := tried[node.ID]; ok {
			continue
		}
		candidate := cloneNodeWithReservation(node, p.reservations[node.ID])
		if candidate.MaxCalls > 0 && candidate.CurrentCalls >= candidate.MaxCalls {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, sdk.ErrNoNode
	}
	node, err := sdk.BalancerFor(policy).Pick(filtered, key)
	if err != nil {
		return nil, err
	}
	p.reservations[node.ID]++
	return node, nil
}

func (p *Pool) releaseReservation(nodeID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releaseReservationLocked(nodeID)
}

func (p *Pool) releaseReservationLocked(nodeID string) {
	if strings.TrimSpace(nodeID) == "" {
		return
	}
	count := p.reservations[nodeID]
	if count <= 1 {
		delete(p.reservations, nodeID)
		return
	}
	p.reservations[nodeID] = count - 1
}

func cloneNodeWithReservation(node *model.Node, reserved int) *model.Node {
	if node == nil {
		return nil
	}
	clone := *node
	if reserved > 0 {
		clone.CurrentCalls += reserved
	}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dialPBXControl(ctx context.Context, url string, handler pbxprotocol.Handler) (controlClient, error) {
	return pbxprotocol.Dial(ctx, url, handler)
}

