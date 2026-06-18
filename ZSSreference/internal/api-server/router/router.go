//  PBX节点路由策略：为新同传会话选择承载媒体节点
package router

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

var ErrNoAvailableNode = errors.New("no available node")

type RouteStrategy string

const (
	RouteStrategyRoundRobin         RouteStrategy = "round_robin"
	RouteStrategyWeightedRoundRobin RouteStrategy = "weighted_round_robin"
	RouteStrategyLeastConnections   RouteStrategy = "least_connections"
	RouteStrategyZoneAffinity       RouteStrategy = "zone_affinity"
	RouteStrategyTenantAffinity     RouteStrategy = "tenant_affinity"
)

type RouteType string

const (
	RouteTypeInternal RouteType = "internal"
	RouteTypeOutbound RouteType = "outbound"
	RouteTypeInbound  RouteType = "inbound"
)

type RouteRequest struct {
	TenantID string        `json:"tenantId"`
	Caller   string        `json:"caller"`
	Callee   string        `json:"callee"`
	Media    string        `json:"media"`
	NeedAI   bool          `json:"needAI"`
	Strategy RouteStrategy `json:"strategy"`
	Zone     string        `json:"zone"`
	Language string        `json:"language"`
	ASRModel string        `json:"asrModel"`
}

type RouteResult struct {
	CallID      string            `json:"callId"`
	RouteType   RouteType         `json:"routeType"`
	GatewayNode string            `json:"gatewayNode"`
	MediaNode   string            `json:"mediaNode"`
	TurnNode    string            `json:"turnNode"`
	AIPipeline  *model.AIPipeline `json:"aiPipeline,omitempty"`
}

type Router struct {
	registry *registry.Registry
	mu       sync.Mutex
	rrIndex  map[string]int
}

// New 创建路由器，内部分配空 round-robin 索引表。
func New(registry *registry.Registry) *Router {
	return &Router{registry: registry, rrIndex: map[string]int{}}
}

// Route 为呼入选择一台可用的媒体节点，并根据策略（round-robin/最少连接/权重/同区亲和）决定节点。开启 AI 时只组装 PBX 负责的 VAD+ASR+TTS 音频能力。
func (r *Router) Route(ctx context.Context, req RouteRequest) (RouteResult, error) {
	if req.Strategy == "" {
		req.Strategy = RouteStrategyRoundRobin
	}

	nodes, err := r.registry.ListNodes(ctx, model.NodeTypeMedia)
	if err != nil {
		return RouteResult{}, err
	}
	candidates := availableMediaNodes(nodes, req)
	if len(candidates) == 0 {
		return RouteResult{}, ErrNoAvailableNode
	}

	selected := r.selectNode(req, candidates)
	result := RouteResult{
		CallID:    idgen.CallID(),
		RouteType: inferRouteType(req),
		MediaNode: selected.ID,
	}

	if req.NeedAI {
		pipeline, err := r.selectAIPipeline(ctx, req, result.CallID)
		if err != nil {
			return RouteResult{}, err
		}
		result.AIPipeline = pipeline
	}

	return result, nil
}

// selectNode 按请求策略从候选节点中选出一个：最少连接、最高权重、同区亲和或 round-robin（默认）。
func (r *Router) selectNode(req RouteRequest, nodes []*model.Node) *model.Node {
	// 先按 ID 排序保证相同输入得到相同结果。
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	switch req.Strategy {
	case RouteStrategyLeastConnections:
		return leastConnections(nodes)
	case RouteStrategyWeightedRoundRobin:
		return highestWeight(nodes)
	case RouteStrategyZoneAffinity:
		for _, node := range nodes {
			if node.Zone == req.Zone {
				return node
			}
		}
		return r.roundRobin(req.TenantID, nodes)
	default:
		return r.roundRobin(req.TenantID, nodes)
	}
}

// roundRobin 按 scope（租户 ID）轮询节点，每个 scope 独立维护一个递增索引。
func (r *Router) roundRobin(scope string, nodes []*model.Node) *model.Node {
	if scope == "" {
		scope = "default"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := r.rrIndex[scope] % len(nodes)
	r.rrIndex[scope]++
	return nodes[idx]
}

// selectAIPipeline 从注册中心各取一个 VAD、ASR、TTS 能力，组装成该通话的音频处理管线。
func (r *Router) selectAIPipeline(ctx context.Context, req RouteRequest, callID string) (*model.AIPipeline, error) {
	vad, err := firstCapability(ctx, r.registry, model.CapabilitySelector{Type: model.CapabilityTypeVAD, Protocol: "grpc"})
	if err != nil {
		return nil, err
	}
	asr, err := firstCapability(ctx, r.registry, model.CapabilitySelector{Type: model.CapabilityTypeASR, Protocol: "grpc", Language: req.Language, Model: req.ASRModel})
	if err != nil {
		return nil, err
	}
	tts, err := firstCapability(ctx, r.registry, model.CapabilitySelector{Type: model.CapabilityTypeTTS, Protocol: "grpc", Language: req.Language})
	if err != nil {
		return nil, err
	}

	return &model.AIPipeline{
		CallID:   callID,
		TenantID: req.TenantID,
		VAD:      vad.ID,
		ASR:      asr.ID,
		TTS:      tts.ID,
		State:    model.AIStateIdle,
	}, nil
}

// availableMediaNodes 过滤出状态 up 且未达到最大通话数的媒体节点。
func availableMediaNodes(nodes []*model.Node, req RouteRequest) []*model.Node {
	out := make([]*model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Status != model.NodeStatusUp {
			continue
		}
		if node.MaxCalls > 0 && node.CurrentCalls >= node.MaxCalls {
			continue
		}
		out = append(out, node)
	}
	return out
}

// leastConnections 返回当前通话数最少的节点。
func leastConnections(nodes []*model.Node) *model.Node {
	best := nodes[0]
	for _, node := range nodes[1:] {
		if node.CurrentCalls < best.CurrentCalls {
			best = node
		}
	}
	return best
}

// highestWeight 返回权重最高的节点。
func highestWeight(nodes []*model.Node) *model.Node {
	best := nodes[0]
	for _, node := range nodes[1:] {
		if node.Weight > best.Weight {
			best = node
		}
	}
	return best
}

// firstCapability 列出匹配选择器的能力并返回第一个。
func firstCapability(ctx context.Context, registry *registry.Registry, selector model.CapabilitySelector) (model.Capability, error) {
	capabilities, err := registry.ListCapabilities(ctx, selector)
	if err != nil {
		return model.Capability{}, err
	}
	if len(capabilities) == 0 {
		return model.Capability{}, ErrNoAvailableNode
	}
	return capabilities[0], nil
}

// inferRouteType 根据被叫号码前缀推断呼叫方向：+ 开头为出局（outbound），否则为内部（internal）。
func inferRouteType(req RouteRequest) RouteType {
	if strings.HasPrefix(strings.TrimSpace(req.Callee), "+") {
		return RouteTypeOutbound
	}
	return RouteTypeInternal
}

