//  api-server 同传业务主服务入口：加载配置→ SQLite→ 注册中心→ LLM客户端→ HTTP服务→ 优雅关闭
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	apiapp "github.com/SATA260/SimulSpeak1/internal/api-server"
	"github.com/SATA260/SimulSpeak1/internal/api-server/httpapi"
	"github.com/SATA260/SimulSpeak1/internal/api-server/pbxcontrol"
	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/bootstrap"
	"github.com/SATA260/SimulSpeak1/internal/config"
	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/logging"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/session"
	sdk "github.com/SATA260/SimulSpeak1/pkg/client"
)

// @title SimulSpeak API Server
// @version 0.1.0
// @description SimulSpeak 控制面 HTTP API：节点注册、路由调度、会话与配置中心。
// @BasePath /
// @schemes http
// @accept json
// @produce json
// main 是服务入口：加载配置 → 启动信号处理 → 调用 bootstrap 生命周期。
func main() {
	logger := logging.NewJSONLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)
	cfg, err := bootstrap.LoadServiceConfig("api-server", os.Args[1:])
	if err != nil {
		logger.Error("加载配置失败", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sessionStore, err := apiapp.OpenSessionStore(ctx, cfg.Database.SQLite.DSN, logger)
	if err != nil {
		logger.Error("初始化 SQLite 会话数据库失败", slog.Any("error", err))
		os.Exit(1)
	}

	dialTimeout, err := parseDurationDefault(cfg.Etcd.DialTimeout, 5*time.Second)
	if err != nil {
		_ = sessionStore.Close()
		logger.Error("解析 etcd dial timeout 失败", slog.Any("error", err))
		os.Exit(1)
	}
	client, err := etcdutil.NewClient(cfg.Etcd.Mode, cfg.Etcd.Endpoints, dialTimeout)
	if err != nil {
		_ = sessionStore.Close()
		logger.Error("初始化 registry client 失败", slog.String("mode", cfg.Etcd.Mode), slog.Any("error", err))
		os.Exit(1)
	}
	reg := registry.New(client, registry.Options{})
	if err := seedMemoryPBXNode(ctx, cfg, reg); err != nil {
		_ = sessionStore.Close()
		_ = client.Close()
		logger.Error("注册本地 PBX fallback 节点失败", slog.Any("error", err))
		os.Exit(1)
	}
	mediaPool := sdk.NewNodePool(reg, model.NodeTypeMedia)
	if err := mediaPool.Start(ctx); err != nil {
		_ = sessionStore.Close()
		_ = client.Close()
		logger.Error("启动 PBX media 节点池失败", slog.Any("error", err))
		os.Exit(1)
	}
	logEffectiveAIConfig(logger, cfg)
	apiHandler := httpapi.New(httpapi.Dependencies{
		Registry:     reg,
		Router:       router.New(reg),
		Config:       configcenter.New(client),
		Sessions:     session.New(client),
		SessionStore: sessionStore,
		LLM:          llm.NewClient(llmTranslateConfig(cfg.AI.LLM)),
		MediaPicker:  mediaPool,
	})
	pbxControl := pbxcontrol.NewPool(mediaPool, apiHandler.HandlePBXMessage, string(sdk.PolicyLeastLoad))
	apiHandler.SetPBXControl(pbxControl)
	server := &http.Server{
		Addr:    cfg.HTTP.Address,
		Handler: apiHandler,
	}

	if err := bootstrap.Run(ctx, bootstrap.App{
		Name:     cfg.Service.Name,
		Start:    startHTTP(logger, server),
		Shutdown: shutdownAPI(logger, server, pbxControl, sessionStore, client),
	}); err != nil {
		logger.Error("服务异常停止", slog.Any("error", err))
		os.Exit(1)
	}
}

func logEffectiveAIConfig(logger *slog.Logger, cfg *config.AppConfig) {
	logger.Info("api-server AI 配置已加载",
		slog.String("pbxNodeWSURL", cfg.PBX.NodeWSURL),
		slog.String("etcdMode", cfg.Etcd.Mode),
		slog.String("llmProvider", cfg.AI.LLM.Provider),
		slog.Bool("llmEndpointConfigured", strings.TrimSpace(cfg.AI.LLM.Endpoint) != ""),
		slog.Bool("llmAPIKeyConfigured", strings.TrimSpace(cfg.AI.LLM.APIKey) != ""),
		slog.Bool("llmModelConfigured", strings.TrimSpace(cfg.AI.LLM.Model) != ""),
	)
}

// startHTTP 返回启动 HTTP 服务器的函数。
func startHTTP(logger *slog.Logger, server *http.Server) func(context.Context) error {
	return func(ctx context.Context) error {
		logger.InfoContext(ctx, "HTTP 服务启动中", slog.String("address", server.Addr))
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.ErrorContext(ctx, "HTTP 服务异常停止", slog.Any("error", err))
			}
		}()
		return nil
	}
}

// shutdownHTTP 返回优雅关闭 HTTP 服务器的函数。
func shutdownHTTP(logger *slog.Logger, server *http.Server) func(context.Context) error {
	return func(ctx context.Context) error {
		logger.InfoContext(ctx, "HTTP 服务停止中", slog.String("address", server.Addr))
		return server.Shutdown(ctx)
	}
}

func shutdownAPI(logger *slog.Logger, server *http.Server, pbxControl interface{ Close() error }, sessionStore interface{ Close() error }, kv interface{ Close() error }) func(context.Context) error {
	return func(ctx context.Context) error {
		var shutdownErr error
		shutdownErr = errors.Join(shutdownErr, shutdownHTTP(logger, server)(ctx))
		if pbxControl != nil {
			if err := pbxControl.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 PBX control 连接失败: %w", err))
			}
		}
		if sessionStore != nil {
			if err := sessionStore.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 SQLite 会话数据库失败: %w", err))
			}
		}
		if kv != nil {
			if err := kv.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 registry client 失败: %w", err))
			}
		}
		return shutdownErr
	}
}

func llmTranslateConfig(provider config.AIProviderConfig) llm.Config {
	return llm.Config{
		Provider: provider.Provider,
		APIKey:   provider.APIKey,
		Endpoint: provider.Endpoint,
		Model:    provider.Model,
		Params:   provider.Params,
		Secrets: map[string]string{
			"apiKey":    provider.APIKey,
			"secretId":  provider.SecretID,
			"secretKey": provider.SecretKey,
		},
	}
}

func seedMemoryPBXNode(ctx context.Context, cfg *config.AppConfig, reg *registry.Registry) error {
	if strings.EqualFold(strings.TrimSpace(cfg.Etcd.Mode), etcdutil.ModeEtcd) {
		return nil
	}
	endpoint := strings.TrimSpace(cfg.Node.Advertise)
	if endpoint == "" {
		endpoint = strings.TrimSpace(cfg.PBX.NodeWSURL)
	}
	if endpoint == "" {
		return nil
	}
	nodeID := strings.TrimSpace(cfg.Node.ID)
	if nodeID == "" {
		nodeID = "local-" + idgen.NodeID()
	}
	maxCalls := cfg.Node.MaxCalls
	if maxCalls <= 0 {
		maxCalls = 100
	}
	weight := cfg.Node.Weight
	if weight <= 0 {
		weight = 1
	}
	return reg.Register(ctx, &model.Node{
		ID:           nodeID,
		Type:         model.NodeTypeMedia,
		Endpoint:     endpoint,
		Zone:         strings.TrimSpace(cfg.Node.Zone),
		Status:       model.NodeStatusUp,
		Weight:       weight,
		MaxCalls:     maxCalls,
		CurrentCalls: 0,
		StartedAt:    time.Now().UTC(),
		Capabilities: []string{"webrtc", "asr", "tmt", "tts", "vad"},
	})
}

func parseDurationDefault(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", value, err)
	}
	return parsed, nil
}

