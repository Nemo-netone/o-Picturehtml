// pbx-node 媒体节点入口：创建WebRTC管理器→ VAD检测器→ 注册到etcd→ 启动HTTP控制面
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/ai/vad"
	"github.com/SATA260/SimulSpeak1/internal/bootstrap"
	"github.com/SATA260/SimulSpeak1/internal/config"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/idgen"
	"github.com/SATA260/SimulSpeak1/internal/logging"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxhttpapi "github.com/SATA260/SimulSpeak1/internal/pbx/httpapi"
	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
	"github.com/SATA260/SimulSpeak1/internal/pbx/webrtc"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// mediaNode 聚合 PBX 媒体平面运行态：WebRTC 上下行管理器与可选模型 VAD 检测器。
type mediaNode struct {
	webrtc     *webrtc.Manager
	detector   *vad.Detector
	httpServer *http.Server
	registry   *registry.Registry
	kv         etcdutil.Client
	nodeID     string
}

// main 是媒体节点入口：加载配置 → 启动信号处理 → 调用 bootstrap 生命周期。
func main() {
	logger := logging.NewJSONLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)
	cfg, err := bootstrap.LoadServiceConfig("pbx-node", os.Args[1:])
	if err != nil {
		logger.Error("加载配置失败", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	node := &mediaNode{}
	providerConfigs := config.ProviderConfigsFromAIConfig(cfg.AI)
	logEffectiveAIConfig(logger, cfg, providerConfigs)
	if err := bootstrap.Run(ctx, bootstrap.App{
		Name:     cfg.Service.Name,
		Start:    startMediaNode(logger, cfg.Service.Name, cfg.Etcd, cfg.Node, cfg.PBX, cfg.HTTP, cfg.GRPC, cfg.AI.VAD, cfg.Recording, providerConfigs, node),
		Shutdown: shutdownMediaNode(logger, cfg.Service.Name, node),
	}); err != nil {
		logger.Error("服务异常停止", slog.Any("error", err))
		os.Exit(1)
	}
}

func logEffectiveAIConfig(logger *slog.Logger, cfg *config.AppConfig, providerConfigs map[model.CapabilityType]model.ProviderConfig) {
	asrConfig := providerConfigs[model.CapabilityTypeASR]
	tmtConfig := providerConfigs[model.CapabilityTypeTMT]
	ttsConfig := providerConfigs[model.CapabilityTypeTTS]
	logger.Info("AI provider 配置已加载",
		slog.String("asrProvider", cfg.AI.ASR.Provider),
		slog.String("asrProviderConfig", asrConfig.Provider),
		slog.Bool("asrEndpointConfigured", asrConfig.Endpoint != ""),
		slog.Bool("asrAppIdConfigured", strings.TrimSpace(asrConfig.Params["appId"]) != ""),
		slog.Bool("asrSecretIdConfigured", strings.TrimSpace(asrConfig.Secrets["secretId"]) != ""),
		slog.Bool("asrSecretKeyConfigured", strings.TrimSpace(asrConfig.Secrets["secretKey"]) != ""),
		slog.String("tmtProvider", cfg.AI.TMT.Provider),
		slog.String("tmtProviderConfig", tmtConfig.Provider),
		slog.Bool("tmtEndpointConfigured", tmtConfig.Endpoint != ""),
		slog.String("tmtRegion", tmtConfig.Params["region"]),
		slog.Bool("tmtSecretIdConfigured", strings.TrimSpace(tmtConfig.Secrets["secretId"]) != ""),
		slog.Bool("tmtSecretKeyConfigured", strings.TrimSpace(tmtConfig.Secrets["secretKey"]) != ""),
		slog.String("ttsProvider", cfg.AI.TTS.Provider),
		slog.String("ttsProviderConfig", ttsConfig.Provider),
		slog.Bool("ttsEndpointConfigured", ttsConfig.Endpoint != ""),
		slog.Bool("ttsAppIdConfigured", strings.TrimSpace(ttsConfig.Params["appId"]) != ""),
		slog.Bool("ttsSecretIdConfigured", strings.TrimSpace(ttsConfig.Secrets["secretId"]) != ""),
		slog.Bool("ttsSecretKeyConfigured", strings.TrimSpace(ttsConfig.Secrets["secretKey"]) != ""),
		slog.String("vadProvider", cfg.AI.VAD.Provider),
		slog.Bool("vadModelConfigured", cfg.AI.VAD.ModelPath != ""),
		slog.Bool("vadRuntimeConfigured", cfg.AI.VAD.RuntimeLibraryPath != ""),
	)
}

// startMediaNode 初始化 WebRTC 管理器与 VAD 检测器，构成媒体上行处理管线。
func startMediaNode(logger *slog.Logger, service string, etcdCfg config.EtcdConfig, nodeCfg config.NodeConfig, pbxCfg config.PBXConfig, httpCfg config.HTTPConfig, grpcCfg config.GRPCConfig, vadCfg config.VADConfig, recordingCfg config.RecordingConfig, providerConfigs map[model.CapabilityType]model.ProviderConfig, node *mediaNode) func(context.Context) error {
	return func(ctx context.Context) error {
		manager, detector, err := newWebRTCManager(vadCfg, pbxCfg, recordingCfg, providerConfigs)
		if err != nil {
			return err
		}
		dialTimeout, err := parseDurationDefault(etcdCfg.DialTimeout, 5*time.Second)
		if err != nil {
			return err
		}
		kv, err := etcdutil.NewClient(etcdCfg.Mode, etcdCfg.Endpoints, dialTimeout)
		if err != nil {
			return err
		}
		reg := registry.New(kv, registry.Options{})
		node.webrtc = manager
		node.detector = detector
		node.kv = kv
		node.registry = reg
		handler := pbxhttpapi.New(pbxhttpapi.Dependencies{
			WebRTC:          manager,
			ProviderConfigs: providerConfigs,
		})
		server := &http.Server{Addr: pbxCfg.ControlAddress, Handler: handler}
		node.httpServer = server
		go func() {
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.ErrorContext(ctx, "PBX control 服务异常停止", slog.Any("error", err))
			}
		}()
		registeredNode := mediaRegistryNode(nodeCfg, pbxCfg)
		if err := reg.Register(ctx, registeredNode); err != nil {
			_ = server.Shutdown(context.Background())
			_ = kv.Close()
			return fmt.Errorf("注册 PBX media 节点失败: %w", err)
		}
		node.nodeID = registeredNode.ID
		go reportMediaNodeLoad(ctx, logger, reg, registeredNode.ID, manager)
		logger.InfoContext(ctx, "PBX 媒体节点已启动",
			slog.String("service", service),
			slog.String("nodeID", registeredNode.ID),
			slog.String("nodeAdvertise", registeredNode.Endpoint),
			slog.String("etcdMode", etcdCfg.Mode),
			slog.String("controlAddress", pbxCfg.ControlAddress),
			slog.String("controlPort", addressPort(pbxCfg.ControlAddress)),
			slog.Int("webrtcUDPPortMin", pbxCfg.WebRTCUDPPortMin),
			slog.Int("webrtcUDPPortMax", pbxCfg.WebRTCUDPPortMax),
			slog.String("httpAddress", httpCfg.Address),
			slog.String("httpPort", addressPort(httpCfg.Address)),
			slog.String("grpcAddress", grpcCfg.Address),
			slog.String("grpcPort", addressPort(grpcCfg.Address)),
			slog.String("vadProvider", vadCfg.Provider),
			slog.Int("vadSampleRate", vadCfg.SampleRate),
			slog.Bool("sileroVADEnabled", detector != nil),
			slog.Bool("recordingEnabled", recordingCfg.Enabled),
			slog.String("recordingDirectory", recordingCfg.Directory),
			slog.Int("recordingSampleRate", recordingCfg.SampleRate),
		)
		return nil
	}
}

func mediaRegistryNode(nodeCfg config.NodeConfig, pbxCfg config.PBXConfig) *model.Node {
	nodeID := strings.TrimSpace(nodeCfg.ID)
	if nodeID == "" {
		nodeID = idgen.NodeID()
	}
	advertise := strings.TrimSpace(nodeCfg.Advertise)
	if advertise == "" {
		advertise = strings.TrimSpace(pbxCfg.NodeWSURL)
	}
	maxCalls := nodeCfg.MaxCalls
	if maxCalls <= 0 {
		maxCalls = 100
	}
	weight := nodeCfg.Weight
	if weight <= 0 {
		weight = 1
	}
	return &model.Node{
		ID:           nodeID,
		Type:         model.NodeTypeMedia,
		Endpoint:     advertise,
		Zone:         strings.TrimSpace(nodeCfg.Zone),
		Status:       model.NodeStatusUp,
		Weight:       weight,
		MaxCalls:     maxCalls,
		CurrentCalls: 0,
		StartedAt:    time.Now().UTC(),
		Capabilities: []string{"webrtc", "asr", "tmt", "tts", "vad"},
	}
}

func reportMediaNodeLoad(ctx context.Context, logger *slog.Logger, reg *registry.Registry, nodeID string, manager *webrtc.Manager) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reg.UpdateLoad(ctx, model.NodeTypeMedia, nodeID, manager.ActiveConnections()); err != nil {
				logger.WarnContext(ctx, "PBX media 节点负载上报失败",
					slog.String("nodeID", nodeID),
					slog.Any("error", err),
				)
			}
		}
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

func addressPort(address string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err == nil {
		return port
	}
	index := strings.LastIndex(address, ":")
	if index < 0 || index == len(address)-1 {
		return ""
	}
	return address[index+1:]
}

func newWebRTCManager(vadCfg config.VADConfig, pbxCfg config.PBXConfig, recordingCfg config.RecordingConfig, providerConfigs map[model.CapabilityType]model.ProviderConfig) (*webrtc.Manager, *vad.Detector, error) {
	managerOptions := []webrtc.ManagerOption{
		webrtc.WithProviderConfigs(providerConfigs),
		webrtc.WithICEPortRange(pbxCfg.WebRTCUDPPortMin, pbxCfg.WebRTCUDPPortMax),
	}
	objectStorage := storage.NewLocalStorage(recordingCfg.Directory)
	managerOptions = append(managerOptions, webrtc.WithRecording(recording.NewService(objectStorage), recordingCfg.Enabled, recordingCfg.SampleRate))
	provider := strings.ToLower(strings.TrimSpace(vadCfg.Provider))
	if provider != vad.ProviderSilero {
		return webrtc.NewManager(managerOptions...), nil, nil
	}
	detector, err := vad.NewDetector(vad.Config{
		Provider:           provider,
		ModelPath:          vadCfg.ModelPath,
		RuntimeLibraryPath: vadCfg.RuntimeLibraryPath,
		SampleRate:         vadCfg.SampleRate,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 Silero VAD 检测器失败: %w", err)
	}
	managerOptions = append(managerOptions, webrtc.WithVADDetector(detector))
	return webrtc.NewManager(managerOptions...), detector, nil
}

// shutdownMediaNode 释放 VAD 检测器与媒体连接。
func shutdownMediaNode(logger *slog.Logger, service string, node *mediaNode) func(context.Context) error {
	return func(ctx context.Context) error {
		var shutdownErr error
		if node.registry != nil && node.nodeID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := node.registry.Deregister(ctx, model.NodeTypeMedia, node.nodeID); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("注销 PBX media 节点失败: %w", err))
			}
			cancel()
			node.registry = nil
			node.nodeID = ""
		}
		if node.httpServer != nil {
			if err := node.httpServer.Shutdown(ctx); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 PBX control 服务失败: %w", err))
			}
			node.httpServer = nil
		}
		if node.detector != nil {
			if err := node.detector.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 VAD 检测器失败: %w", err))
			}
			node.detector = nil
		}
		if node.kv != nil {
			if err := node.kv.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("关闭 registry client 失败: %w", err))
			}
			node.kv = nil
		}
		logger.InfoContext(ctx, "PBX 媒体节点已停止", slog.String("service", service))
		return shutdownErr
	}
}
