//  ICE候选处理：NAT穿透候选收集与优选
package ice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

type Options struct {
	SharedSecret  string
	DefaultSTUN   []string
	CredentialTTL time.Duration
}

type Request struct {
	TenantID string
	Zone     string
}

type Config struct {
	STUNServers []STUNServer
	TURNServers []TURNServer
}

type STUNServer struct {
	URLs []string
}

type TURNServer struct {
	NodeID     string
	URLs       []string
	Username   string
	Credential string
	ExpiresAt  time.Time
}

type Credential struct {
	Username  string
	Password  string
	ExpiresAt time.Time
	TenantID  string
}

type Service struct {
	registry *registry.Registry
	options  Options
}

// New 创建 ICE/TURN 凭证服务，默认凭证有效期 1 小时。
func New(registry *registry.Registry, options Options) *Service {
	if options.CredentialTTL <= 0 {
		options.CredentialTTL = time.Hour
	}
	return &Service{registry: registry, options: options}
}

// GetICEConfig 列出可用的 STUN/TURN 服务器，为每个健康 TURN 节点生成限时凭证。同区节点优先。
func (s *Service) GetICEConfig(ctx context.Context, req Request) (Config, error) {
	nodes, err := s.registry.ListNodes(ctx, model.NodeTypeTurn)
	if err != nil {
		return Config{}, err
	}
	nodes = healthyTurns(nodes)
	// 同区 TURN 节点排在前面。
	sort.SliceStable(nodes, func(i, j int) bool {
		if req.Zone != "" && nodes[i].Zone != nodes[j].Zone {
			return nodes[i].Zone == req.Zone
		}
		return nodes[i].ID < nodes[j].ID
	})

	cfg := Config{STUNServers: []STUNServer{{URLs: s.options.DefaultSTUN}}}
	for _, node := range nodes {
		cred := s.GenerateCredential(req.TenantID, s.options.CredentialTTL)
		cfg.TURNServers = append(cfg.TURNServers, TURNServer{
			NodeID:     node.ID,
			URLs:       []string{"turn:" + node.Endpoint},
			Username:   cred.Username,
			Credential: cred.Password,
			ExpiresAt:  cred.ExpiresAt,
		})
	}
	return cfg, nil
}

// GenerateCredential 生成 coturn 兼容的限时 TURN 凭证（HMAC-SHA1 签名）。
func (s *Service) GenerateCredential(tenantID string, ttl time.Duration) Credential {
	expires := time.Now().Add(ttl).UTC()
	username := fmt.Sprintf("%d:%s", expires.Unix(), tenantID)
	mac := hmac.New(sha1.New, []byte(s.options.SharedSecret))
	_, _ = mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return Credential{Username: username, Password: password, ExpiresAt: expires, TenantID: tenantID}
}

// ValidateCredential 校验 TURN 凭证：检查用户名格式、过期时间和 HMAC 签名。
func (s *Service) ValidateCredential(credential Credential, now time.Time) bool {
	parts := strings.SplitN(credential.Username, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if now.Unix() > expiresUnix {
		return false
	}
	expected := s.GenerateCredentialAt(parts[1], time.Unix(expiresUnix, 0).UTC())
	return hmac.Equal([]byte(expected.Password), []byte(credential.Password))
}

// GenerateCredentialAt 在指定过期时间生成凭证（用于验证时回算签名）。
func (s *Service) GenerateCredentialAt(tenantID string, expires time.Time) Credential {
	username := fmt.Sprintf("%d:%s", expires.Unix(), tenantID)
	mac := hmac.New(sha1.New, []byte(s.options.SharedSecret))
	_, _ = mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return Credential{Username: username, Password: password, ExpiresAt: expires, TenantID: tenantID}
}

// healthyTurns 过滤出状态为 up 的 TURN 节点。
func healthyTurns(nodes []*model.Node) []*model.Node {
	out := make([]*model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Status == model.NodeStatusUp {
			out = append(out, node)
		}
	}
	return out
}

