// worker 异步任务处理器入口：承载会后总结、字幕导出等离线任务
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/llm"
	apiapp "github.com/SATA260/SimulSpeak1/internal/api-server"
	"github.com/SATA260/SimulSpeak1/internal/bootstrap"
	"github.com/SATA260/SimulSpeak1/internal/config"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/logging"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/registry"
	"github.com/SATA260/SimulSpeak1/internal/worker/vocabulary"
)

type workerRuntime struct {
	kv       etcdutil.Client
	registry *registry.Registry
	nodeID   string
}

// main 是异步任务 worker 入口：承载会后总结、字幕导出等离线任务。
func main() {
	logger := logging.NewJSONLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)
	cfg, err := bootstrap.LoadServiceConfig("worker", os.Args[1:])
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
	workerOptions := vocabularyOptionsFromEnv()
	consumer := vocabulary.NewConsumer(
		sessionStore,
		llm.NewClient(llmTranslateConfig(cfg.AI.LLM)),
		workerOptions,
		logger,
	)
	runtime := &workerRuntime{}
	done := make(chan error, 1)

	if err := bootstrap.Run(ctx, bootstrap.App{
		Name: cfg.Service.Name,
		Start: func(ctx context.Context) error {
			if err := startWorkerRegistry(ctx, logger, cfg, consumer, runtime); err != nil {
				return err
			}
			logger.InfoContext(ctx, "worker 已启动",
				slog.String("service", cfg.Service.Name),
				slog.String("nodeID", runtime.nodeID),
				slog.String("endpoint", "worker://"+runtime.nodeID),
				slog.String("etcdMode", cfg.Etcd.Mode),
				slog.Int("concurrency", consumer.Concurrency()),
			)
			go func() {
				done <- consumer.Run(ctx)
			}()
			return nil
		},
		Shutdown: func(ctx context.Context) error {
			logger.InfoContext(ctx, "worker 已停止", slog.String("service", cfg.Service.Name))
			var shutdownErr error
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					shutdownErr = errors.Join(shutdownErr, err)
				}
			case <-ctx.Done():
				shutdownErr = errors.Join(shutdownErr, ctx.Err())
			}
			if err := sessionStore.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 SQLite 会话数据库失败: %w", err))
			}
			if err := shutdownWorkerRegistry(ctx, runtime); err != nil {
				shutdownErr = errors.Join(shutdownErr, err)
			}
			return shutdownErr
		},
	}); err != nil {
		logger.Error("服务异常停止", slog.Any("error", err))
		os.Exit(1)
	}
}

func startWorkerRegistry(ctx context.Context, logger *slog.Logger, cfg *config.AppConfig, consumer *vocabulary.Consumer, runtime *workerRuntime) error {
	dialTimeout, err := parseDurationDefault(cfg.Etcd.DialTimeout, 5*time.Second)
	if err != nil {
		return err
	}
	kv, err := etcdutil.NewClient(cfg.Etcd.Mode, cfg.Etcd.Endpoints, dialTimeout)
	if err != nil {
		return fmt.Errorf("初始化 registry client 失败: %w", err)
	}
	reg := registry.New(kv, registry.Options{})
	node := workerRegistryNode(cfg, consumer)
	if err := reg.Register(ctx, node); err != nil {
		_ = kv.Close()
		return fmt.Errorf("注册 worker 节点失败: %w", err)
	}
	runtime.kv = kv
	runtime.registry = reg
	runtime.nodeID = node.ID
	go reportWorkerNodeLoad(ctx, logger, reg, node.ID, consumer)
	return nil
}

func workerRegistryNode(cfg *config.AppConfig, consumer *vocabulary.Consumer) *model.Node {
	nodeID := strings.TrimSpace(os.Getenv("SIMULSPEAK_WORKER_NODE_ID"))
	if nodeID == "" {
		nodeID = defaultWorkerNodeID()
	}
	nodeID = sanitizeNodeID(nodeID)
	concurrency := consumer.Concurrency()
	if concurrency <= 0 {
		concurrency = 1
	}
	weight := cfg.Node.Weight
	if weight <= 0 {
		weight = 1
	}
	return &model.Node{
		ID:           nodeID,
		Type:         model.NodeTypeWorker,
		Endpoint:     "worker://" + nodeID,
		Zone:         strings.TrimSpace(cfg.Node.Zone),
		Status:       model.NodeStatusUp,
		Weight:       weight,
		MaxCalls:     concurrency,
		CurrentCalls: consumer.ActiveTasks(),
		StartedAt:    time.Now().UTC(),
		Capabilities: []string{"vocabulary"},
		Labels: map[string]string{
			"service":  cfg.Service.Name,
			"workerId": envString("SIMULSPEAK_WORKER_ID", "vocabulary-worker"),
		},
	}
}

func defaultWorkerNodeID() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = idgen.NodeID()
	}
	return fmt.Sprintf("%s-%s-%d", envString("SIMULSPEAK_WORKER_ID", "vocabulary-worker"), hostname, os.Getpid())
}

func sanitizeNodeID(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-", "\t", "-", "\n", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return idgen.NodeID()
	}
	return value
}

func reportWorkerNodeLoad(ctx context.Context, logger *slog.Logger, reg *registry.Registry, nodeID string, consumer *vocabulary.Consumer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reg.UpdateLoad(ctx, model.NodeTypeWorker, nodeID, consumer.ActiveTasks()); err != nil {
				logger.WarnContext(ctx, "worker 节点负载上报失败",
					slog.String("nodeID", nodeID),
					slog.Any("error", err),
				)
			}
		}
	}
}

func shutdownWorkerRegistry(ctx context.Context, runtime *workerRuntime) error {
	var shutdownErr error
	if runtime.registry != nil && runtime.nodeID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := runtime.registry.Deregister(ctx, model.NodeTypeWorker, runtime.nodeID); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("注销 worker 节点失败: %w", err))
		}
		cancel()
		runtime.registry = nil
		runtime.nodeID = ""
	}
	if runtime.kv != nil {
		if err := runtime.kv.Close(); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 registry client 失败: %w", err))
		}
		runtime.kv = nil
	}
	return shutdownErr
}

func llmTranslateConfig(provider config.AIProviderConfig) llm.Config {
	return llm.Config{
		Provider: provider.Provider,
		APIKey:   provider.APIKey,
		Endpoint: provider.Endpoint,
		Model:    provider.Model,
		Params:   provider.Params,
		Timeout:  envDuration("SIMULSPEAK_VOCAB_LLM_TIMEOUT", envDuration("SIMULSPEAK_LLM_TIMEOUT", 30*time.Second)),
		Secrets: map[string]string{
			"apiKey":    provider.APIKey,
			"secretId":  provider.SecretID,
			"secretKey": provider.SecretKey,
		},
	}
}

func vocabularyOptionsFromEnv() vocabulary.Options {
	return vocabulary.Options{
		WorkerID:     envString("SIMULSPEAK_WORKER_ID", "vocabulary-worker"),
		Concurrency:  envInt("SIMULSPEAK_WORKER_CONCURRENCY", 1),
		PollInterval: envDuration("SIMULSPEAK_WORKER_POLL_INTERVAL", time.Second),
		LockTimeout:  envDuration("SIMULSPEAK_VOCAB_TASK_LOCK_TIMEOUT", 5*time.Minute),
		MaxAttempts:  envInt("SIMULSPEAK_VOCAB_TASK_MAX_ATTEMPTS", 3),
	}
}

func parseDurationDefault(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("解析 duration %q 失败: %w", value, err)
	}
	return parsed, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
