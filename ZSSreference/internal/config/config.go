// 应用配置结构定义：YAML/环境变量/命令行参数统一配置模型
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const RedactedValue = "[redacted]"

var ErrMissingSecret = errors.New("missing required secret")

type LoadOptions struct {
	Path           string
	EnvPrefix      string
	Args           []string
	RequireSecrets bool
}

type AppConfig struct {
	Service       ServiceConfig       `json:"service" yaml:"service"`
	HTTP          HTTPConfig          `json:"http" yaml:"http"`
	GRPC          GRPCConfig          `json:"grpc" yaml:"grpc"`
	Etcd          EtcdConfig          `json:"etcd" yaml:"etcd"`
	Node          NodeConfig          `json:"node" yaml:"node"`
	PBX           PBXConfig           `json:"pbx" yaml:"pbx"`
	Database      DatabaseConfig      `json:"database" yaml:"database"`
	NATS          NATSConfig          `json:"nats" yaml:"nats"`
	ObjectStorage ObjectStorageConfig `json:"objectStorage" yaml:"objectStorage"`
	Recording     RecordingConfig     `json:"recording" yaml:"recording"`
	JWT           JWTConfig           `json:"jwt" yaml:"jwt"`
	AI            AIConfig            `json:"ai" yaml:"ai"`
	Features      FeatureFlags        `json:"features" yaml:"features"`
}

type ServiceConfig struct {
	Name string `json:"name" yaml:"name"`
}

type HTTPConfig struct {
	Address string `json:"address" yaml:"address"`
}

type GRPCConfig struct {
	Address string `json:"address" yaml:"address"`
}

type EtcdConfig struct {
	Mode        string   `json:"mode" yaml:"mode"`
	Endpoints   []string `json:"endpoints" yaml:"endpoints"`
	DialTimeout string   `json:"dialTimeout" yaml:"dialTimeout"`
}

type NodeConfig struct {
	ID        string `json:"id" yaml:"id"`
	Advertise string `json:"advertise" yaml:"advertise"`
	Zone      string `json:"zone" yaml:"zone"`
	MaxCalls  int    `json:"maxCalls" yaml:"maxCalls"`
	Weight    int    `json:"weight" yaml:"weight"`
}

type PBXConfig struct {
	ControlAddress   string `json:"controlAddress" yaml:"controlAddress"`
	NodeWSURL        string `json:"nodeWsUrl" yaml:"nodeWsUrl"`
	WebRTCUDPPortMin int    `json:"webrtcUdpPortMin" yaml:"webrtcUdpPortMin"`
	WebRTCUDPPortMax int    `json:"webrtcUdpPortMax" yaml:"webrtcUdpPortMax"`
}

type DatabaseConfig struct {
	SQLite DBConfig    `json:"sqlite" yaml:"sqlite"`
	Redis  RedisConfig `json:"redis" yaml:"redis"`
}

type DBConfig struct {
	DSN string `json:"dsn" yaml:"dsn"`
}

type RedisConfig struct {
	Address string `json:"address" yaml:"address"`
}

type NATSConfig struct {
	URL string `json:"url" yaml:"url"`
}

type ObjectStorageConfig struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Bucket   string `json:"bucket" yaml:"bucket"`
}

type RecordingConfig struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	Directory  string `json:"directory" yaml:"directory"`
	SampleRate int    `json:"sampleRate" yaml:"sampleRate"`
}

type JWTConfig struct {
	Issuer     string `json:"issuer" yaml:"issuer"`
	Secret     string `json:"secret,omitempty" yaml:"secret"`
	SecretEnv  string `json:"secretEnv,omitempty" yaml:"secretEnv"`
	SecretFile string `json:"secretFile,omitempty" yaml:"secretFile"`
}

type AIConfig struct {
	VAD VADConfig        `json:"vad" yaml:"vad"`
	ASR AIProviderConfig `json:"asr" yaml:"asr"`
	TMT AIProviderConfig `json:"tmt" yaml:"tmt"`
	TTS AIProviderConfig `json:"tts" yaml:"tts"`
	LLM AIProviderConfig `json:"llm" yaml:"llm"`
}

type VADConfig struct {
	Provider           string `json:"provider" yaml:"provider"`
	ModelPath          string `json:"modelPath" yaml:"modelPath"`
	RuntimeLibraryPath string `json:"runtimeLibraryPath" yaml:"runtimeLibraryPath"`
	SampleRate         int    `json:"sampleRate" yaml:"sampleRate"`
}

type AIProviderConfig struct {
	Provider      string            `json:"provider" yaml:"provider"`
	Endpoint      string            `json:"endpoint" yaml:"endpoint"`
	Model         string            `json:"model,omitempty" yaml:"model"`
	Params        map[string]string `json:"params,omitempty" yaml:"params"`
	APIKey        string            `json:"apiKey,omitempty" yaml:"apiKey"`
	APIKeyEnv     string            `json:"apiKeyEnv,omitempty" yaml:"apiKeyEnv"`
	APIKeyFile    string            `json:"apiKeyFile,omitempty" yaml:"apiKeyFile"`
	AppID         string            `json:"appId,omitempty" yaml:"appId"`
	SecretID      string            `json:"secretId,omitempty" yaml:"secretId"`
	SecretIDEnv   string            `json:"secretIdEnv,omitempty" yaml:"secretIdEnv"`
	SecretIDFile  string            `json:"secretIdFile,omitempty" yaml:"secretIdFile"`
	SecretKey     string            `json:"secretKey,omitempty" yaml:"secretKey"`
	SecretKeyEnv  string            `json:"secretKeyEnv,omitempty" yaml:"secretKeyEnv"`
	SecretKeyFile string            `json:"secretKeyFile,omitempty" yaml:"secretKeyFile"`
}

type FeatureFlags struct {
	Global  map[string]bool            `json:"global" yaml:"global"`
	Tenants map[string]map[string]bool `json:"tenants" yaml:"tenants"`
}

type ConfigChanged struct {
	TenantID string `json:"tenantId,omitempty"`
	Resource string `json:"resource"`
	Version  int64  `json:"version"`
}

// Load 按优先级链加载配置：默认值 → YAML 文件 → 环境变量（SIMULSPEAK_*）→ 命令行参数 → 解析密钥 → 校验。
func Load(opts LoadOptions) (*AppConfig, error) {
	cfg := DefaultConfig()

	path := opts.Path
	if path == "" {
		path = configPathFromArgs(opts.Args)
	}
	if path != "" {
		if err := loadYAML(path, &cfg); err != nil {
			return nil, err
		}
	}

	if opts.EnvPrefix != "" {
		applyEnv(&cfg, opts.EnvPrefix)
	}

	if err := applyFlags(&cfg, opts.Args); err != nil {
		return nil, err
	}

	if err := cfg.ResolveSecrets(); err != nil {
		return nil, err
	}

	if err := cfg.Validate(opts.RequireSecrets); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadEnvFile 读取 dotenv 风格的 KEY=VALUE 文件并写入进程环境，供随后的 Load 读取。
// 规则：跳过空行与 # 注释，支持可选的 export 前缀与成对引号；已存在的环境变量不会被覆盖
// （真实环境优先于 .env 文件）；文件不存在时返回 nil，不视为错误。
func LoadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read env file %s: %w", path, err)
	}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}

	return nil
}

// DefaultConfig 返回全量默认配置（本地开发环境参数）。
func DefaultConfig() AppConfig {
	return AppConfig{
		Service: ServiceConfig{Name: "simulspeak"},
		HTTP:    HTTPConfig{Address: "0.0.0.0:8080"},
		GRPC:    GRPCConfig{Address: "0.0.0.0:9090"},
		Etcd: EtcdConfig{
			Mode:        "memory",
			Endpoints:   []string{"http://127.0.0.1:2379"},
			DialTimeout: "5s",
		},
		Node: NodeConfig{
			Advertise: "ws://127.0.0.1:8081/pbx/ws",
			MaxCalls:  100,
			Weight:    1,
		},
		PBX: PBXConfig{
			ControlAddress:   "0.0.0.0:8081",
			NodeWSURL:        "ws://127.0.0.1:8081/pbx/ws",
			WebRTCUDPPortMin: 20000,
			WebRTCUDPPortMax: 20100,
		},
		Database: DatabaseConfig{
			SQLite: DBConfig{DSN: "file:./data/simulspeak.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"},
			Redis:  RedisConfig{Address: "127.0.0.1:6379"},
		},
		NATS: NATSConfig{URL: "nats://127.0.0.1:4222"},
		ObjectStorage: ObjectStorageConfig{
			Endpoint: "http://127.0.0.1:9000",
			Bucket:   "simulspeak-recordings",
		},
		Recording: RecordingConfig{
			Enabled:    false,
			Directory:  "./data/recordings",
			SampleRate: 16000,
		},
		JWT: JWTConfig{Issuer: "simulspeak"},
		AI: AIConfig{
			VAD: VADConfig{Provider: "simple", ModelPath: "./models/silero-vad.onnx", SampleRate: 16000},
			ASR: AIProviderConfig{Provider: "mock"},
			TMT: AIProviderConfig{Provider: "mock"},
			TTS: AIProviderConfig{Provider: "mock"},
			LLM: AIProviderConfig{Provider: "mock"},
		},
		Features: FeatureFlags{
			Global: map[string]bool{
				"realtimeAI": false,
				"bargeIn":    false,
				"recording":  false,
				"conference": false,
				"billing":    false,
			},
			Tenants: map[string]map[string]bool{},
		},
	}
}

// Validate 校验必填项：service.name 和 etcd.endpoints，可选要求 JWT secret 已设置。
func (cfg AppConfig) Validate(requireSecrets bool) error {
	if cfg.Service.Name == "" {
		return errors.New("service.name is required")
	}
	if len(cfg.Etcd.Endpoints) == 0 {
		return errors.New("etcd.endpoints is required")
	}
	if cfg.Etcd.Mode == "" {
		return errors.New("etcd.mode is required")
	}
	if requireSecrets && cfg.JWT.Secret == "" {
		return fmt.Errorf("jwt.secret: %w", ErrMissingSecret)
	}

	return nil
}

// ResolveSecrets 从环境变量或文件读取 JWT/ASR/TTS 的实际密钥值。
func (cfg *AppConfig) ResolveSecrets() error {
	if err := resolveSecret(&cfg.JWT.Secret, cfg.JWT.SecretEnv, cfg.JWT.SecretFile); err != nil {
		return fmt.Errorf("jwt secret: %w", err)
	}
	if err := resolveSecret(&cfg.AI.ASR.APIKey, cfg.AI.ASR.APIKeyEnv, cfg.AI.ASR.APIKeyFile); err != nil {
		return fmt.Errorf("asr api key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.ASR.SecretID, cfg.AI.ASR.SecretIDEnv, cfg.AI.ASR.SecretIDFile); err != nil {
		return fmt.Errorf("asr secret id: %w", err)
	}
	if err := resolveSecret(&cfg.AI.ASR.SecretKey, cfg.AI.ASR.SecretKeyEnv, cfg.AI.ASR.SecretKeyFile); err != nil {
		return fmt.Errorf("asr secret key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TTS.APIKey, cfg.AI.TTS.APIKeyEnv, cfg.AI.TTS.APIKeyFile); err != nil {
		return fmt.Errorf("tts api key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TTS.SecretID, cfg.AI.TTS.SecretIDEnv, cfg.AI.TTS.SecretIDFile); err != nil {
		return fmt.Errorf("tts secret id: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TTS.SecretKey, cfg.AI.TTS.SecretKeyEnv, cfg.AI.TTS.SecretKeyFile); err != nil {
		return fmt.Errorf("tts secret key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TMT.APIKey, cfg.AI.TMT.APIKeyEnv, cfg.AI.TMT.APIKeyFile); err != nil {
		return fmt.Errorf("tmt api key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TMT.SecretID, cfg.AI.TMT.SecretIDEnv, cfg.AI.TMT.SecretIDFile); err != nil {
		return fmt.Errorf("tmt secret id: %w", err)
	}
	if err := resolveSecret(&cfg.AI.TMT.SecretKey, cfg.AI.TMT.SecretKeyEnv, cfg.AI.TMT.SecretKeyFile); err != nil {
		return fmt.Errorf("tmt secret key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.LLM.APIKey, cfg.AI.LLM.APIKeyEnv, cfg.AI.LLM.APIKeyFile); err != nil {
		return fmt.Errorf("llm api key: %w", err)
	}
	if err := resolveSecret(&cfg.AI.LLM.SecretID, cfg.AI.LLM.SecretIDEnv, cfg.AI.LLM.SecretIDFile); err != nil {
		return fmt.Errorf("llm secret id: %w", err)
	}
	if err := resolveSecret(&cfg.AI.LLM.SecretKey, cfg.AI.LLM.SecretKeyEnv, cfg.AI.LLM.SecretKeyFile); err != nil {
		return fmt.Errorf("llm secret key: %w", err)
	}

	return nil
}

// Redacted 返回脱敏后的配置副本（密钥替换为 [redacted]），用于日志输出。
func (cfg AppConfig) Redacted() AppConfig {
	redacted := cfg
	if redacted.JWT.Secret != "" {
		redacted.JWT.Secret = RedactedValue
	}
	if redacted.AI.ASR.APIKey != "" {
		redacted.AI.ASR.APIKey = RedactedValue
	}
	if redacted.AI.ASR.SecretID != "" {
		redacted.AI.ASR.SecretID = RedactedValue
	}
	if redacted.AI.ASR.SecretKey != "" {
		redacted.AI.ASR.SecretKey = RedactedValue
	}
	if redacted.AI.TTS.APIKey != "" {
		redacted.AI.TTS.APIKey = RedactedValue
	}
	if redacted.AI.TTS.SecretID != "" {
		redacted.AI.TTS.SecretID = RedactedValue
	}
	if redacted.AI.TTS.SecretKey != "" {
		redacted.AI.TTS.SecretKey = RedactedValue
	}
	if redacted.AI.TMT.APIKey != "" {
		redacted.AI.TMT.APIKey = RedactedValue
	}
	if redacted.AI.TMT.SecretID != "" {
		redacted.AI.TMT.SecretID = RedactedValue
	}
	if redacted.AI.TMT.SecretKey != "" {
		redacted.AI.TMT.SecretKey = RedactedValue
	}
	if redacted.AI.LLM.APIKey != "" {
		redacted.AI.LLM.APIKey = RedactedValue
	}
	if redacted.AI.LLM.SecretID != "" {
		redacted.AI.LLM.SecretID = RedactedValue
	}
	if redacted.AI.LLM.SecretKey != "" {
		redacted.AI.LLM.SecretKey = RedactedValue
	}

	return redacted
}

// Enabled 判断某租户的某特性是否启用。租户级覆盖优先于全局设置。
func (flags FeatureFlags) Enabled(tenantID, feature string) bool {
	if tenantFlags, ok := flags.Tenants[tenantID]; ok {
		if enabled, ok := tenantFlags[feature]; ok {
			return enabled
		}
	}

	return flags.Global[feature]
}

// loadYAML 从文件加载 YAML 配置并合并到 cfg。
func loadYAML(path string, cfg *AppConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

// applyEnv 从环境变量（{prefix}_SERVICE_NAME 等）覆盖配置项。
func applyEnv(cfg *AppConfig, prefix string) {
	if value, ok := lookupEnv(prefix, "SERVICE_NAME"); ok {
		cfg.Service.Name = value
	}
	if value, ok := lookupEnv(prefix, "HTTP_ADDRESS"); ok {
		cfg.HTTP.Address = value
	}
	if value, ok := lookupEnv(prefix, "GRPC_ADDRESS"); ok {
		cfg.GRPC.Address = value
	}
	if value, ok := lookupEnv(prefix, "ETCD_ENDPOINTS"); ok {
		cfg.Etcd.Endpoints = splitCSV(value)
	}
	if value, ok := lookupEnv(prefix, "ETCD_MODE"); ok {
		cfg.Etcd.Mode = value
	}
	if value, ok := lookupEnv(prefix, "PBX_CONTROL_ADDRESS"); ok {
		cfg.PBX.ControlAddress = value
	}
	if value, ok := lookupEnv(prefix, "PBX_NODE_WS_URL"); ok {
		cfg.PBX.NodeWSURL = value
		cfg.Node.Advertise = value
	}
	if value, ok := lookupAnyEnv(prefix, "PBX_WEBRTC_UDP_PORT_MIN", "WEBRTC_UDP_PORT_MIN"); ok {
		if port, err := parsePositiveInt(value); err == nil {
			cfg.PBX.WebRTCUDPPortMin = port
		}
	}
	if value, ok := lookupAnyEnv(prefix, "PBX_WEBRTC_UDP_PORT_MAX", "WEBRTC_UDP_PORT_MAX"); ok {
		if port, err := parsePositiveInt(value); err == nil {
			cfg.PBX.WebRTCUDPPortMax = port
		}
	}
	if value, ok := lookupEnv(prefix, "NODE_ID"); ok {
		cfg.Node.ID = value
	}
	if value, ok := lookupEnv(prefix, "NODE_ADVERTISE"); ok {
		cfg.Node.Advertise = value
	}
	if value, ok := lookupEnv(prefix, "NODE_ZONE"); ok {
		cfg.Node.Zone = value
	}
	if value, ok := lookupEnv(prefix, "NODE_MAX_CALLS"); ok {
		if maxCalls, err := parsePositiveInt(value); err == nil {
			cfg.Node.MaxCalls = maxCalls
		}
	}
	if value, ok := lookupEnv(prefix, "NODE_WEIGHT"); ok {
		if weight, err := parsePositiveInt(value); err == nil {
			cfg.Node.Weight = weight
		}
	}
	if value, ok := lookupEnv(prefix, "SQLITE_DSN"); ok {
		cfg.Database.SQLite.DSN = value
	}
	if value, ok := lookupEnv(prefix, "REDIS_ADDRESS"); ok {
		cfg.Database.Redis.Address = value
	}
	if value, ok := lookupEnv(prefix, "NATS_URL"); ok {
		cfg.NATS.URL = value
	}
	if value, ok := lookupEnv(prefix, "RECORDING_ENABLED"); ok {
		if enabled, err := parseBool(value); err == nil {
			cfg.Recording.Enabled = enabled
		}
	}
	if value, ok := lookupEnv(prefix, "RECORDING_DIR"); ok {
		cfg.Recording.Directory = value
	}
	if value, ok := lookupEnv(prefix, "RECORDING_SAMPLE_RATE"); ok {
		if sampleRate, err := parsePositiveInt(value); err == nil {
			cfg.Recording.SampleRate = sampleRate
		}
	}
	if value, ok := lookupEnv(prefix, "JWT_SECRET"); ok {
		cfg.JWT.Secret = value
	}
	applyAIProviderEnv(&cfg.AI.ASR, prefix, "ASR")
	applyAIProviderEnv(&cfg.AI.TMT, prefix, "TMT")
	applyTranslateAliasEnv(&cfg.AI.TMT, prefix)
	applyAIProviderEnv(&cfg.AI.TTS, prefix, "TTS")
	applyAIProviderEnv(&cfg.AI.LLM, prefix, "LLM")
	applyLLMProviderEnv(&cfg.AI.LLM, prefix)
	if value, ok := lookupEnv(prefix, "VAD_PROVIDER"); ok {
		cfg.AI.VAD.Provider = value
	}
	if value, ok := lookupEnv(prefix, "VAD_MODEL_PATH"); ok {
		cfg.AI.VAD.ModelPath = value
	}
	if value, ok := lookupEnv(prefix, "VAD_RUNTIME_LIBRARY_PATH"); ok {
		cfg.AI.VAD.RuntimeLibraryPath = value
	}
	if value, ok := lookupEnv(prefix, "ONNX_RUNTIME_LIBRARY_PATH"); ok {
		cfg.AI.VAD.RuntimeLibraryPath = value
	}
	if value, ok := lookupEnv(prefix, "VAD_SAMPLE_RATE"); ok {
		if sampleRate, err := parsePositiveInt(value); err == nil {
			cfg.AI.VAD.SampleRate = sampleRate
		}
	}
}

func applyAIProviderEnv(provider *AIProviderConfig, prefix, capability string) {
	if value, ok := lookupEnv(prefix, capability+"_PROVIDER"); ok {
		provider.Provider = value
	}
	if value, ok := lookupEnv(prefix, capability+"_ENDPOINT"); ok {
		provider.Endpoint = value
	}
	if vendor := providerEnvVendor(provider.Provider); vendor != "" {
		applyProviderSpecificEnv(provider, prefix, capability, vendor)
	}
}

func applyLLMProviderEnv(provider *AIProviderConfig, prefix string) {
	if value, ok := lookupEnv(prefix, "LLM_API_KEY"); ok {
		provider.APIKey = value
	}
	if value, ok := lookupEnv(prefix, "LLM_MODEL"); ok {
		provider.Model = value
	}
}

func applyTranslateAliasEnv(provider *AIProviderConfig, prefix string) {
	if _, ok := lookupEnv(prefix, "TMT_PROVIDER"); !ok {
		if value, exists := lookupEnv(prefix, "TRANSLATE_PROVIDER"); exists {
			provider.Provider = value
		}
	}
	if _, ok := lookupEnv(prefix, "TMT_ENDPOINT"); !ok {
		if value, exists := lookupEnv(prefix, "TRANSLATE_ENDPOINT"); exists {
			provider.Endpoint = value
		}
	}
	if vendor := providerEnvVendor(provider.Provider); vendor != "" {
		applyProviderSpecificEnv(provider, prefix, "TMT", vendor)
	}
}

func applyProviderSpecificEnv(provider *AIProviderConfig, prefix, capability, vendor string) {
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_ENDPOINT", vendor+"_"+capability+"_BASEURL", vendor+"_"+capability+"_BASE_URL", capability+"_"+vendor+"_ENDPOINT", capability+"_"+vendor+"_BASEURL", capability+"_"+vendor+"_BASE_URL"); ok {
		provider.Endpoint = value
	}
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_APPID", vendor+"_"+capability+"_APP_ID", capability+"_"+vendor+"_APPID", capability+"_"+vendor+"_APP_ID"); ok {
		provider.AppID = value
	}
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_SECRETID", vendor+"_"+capability+"_SECRET_ID", capability+"_"+vendor+"_SECRETID", capability+"_"+vendor+"_SECRET_ID"); ok {
		provider.SecretID = value
	}
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_SECRETKEY", vendor+"_"+capability+"_SECRET_KEY", capability+"_"+vendor+"_SECRETKEY", capability+"_"+vendor+"_SECRET_KEY"); ok {
		provider.SecretKey = value
	}
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_REGION", capability+"_"+vendor+"_REGION", capability+"_REGION"); ok {
		ensureProviderParams(provider)["region"] = value
	}
	if value, ok := lookupAnyEnv(prefix, vendor+"_"+capability+"_PROJECT_ID", vendor+"_"+capability+"_PROJECTID", capability+"_"+vendor+"_PROJECT_ID", capability+"_"+vendor+"_PROJECTID", capability+"_PROJECT_ID", capability+"_PROJECTID"); ok {
		ensureProviderParams(provider)["projectId"] = value
	}
}

func ensureProviderParams(provider *AIProviderConfig) map[string]string {
	if provider.Params == nil {
		provider.Params = map[string]string{}
	}
	return provider.Params
}

func providerEnvVendor(provider string) string {
	name := strings.ToUpper(strings.TrimSpace(provider))
	switch {
	case strings.HasPrefix(name, "TENCENT"):
		return "TENCENT"
	case strings.HasPrefix(name, "ALIYUN"):
		return "ALIYUN"
	case strings.HasPrefix(name, "AZURE"):
		return "AZURE"
	default:
		return ""
	}
}

// applyFlags 从命令行参数（--service-name 等）覆盖配置项。
func applyFlags(cfg *AppConfig, args []string) error {
	fs := flag.NewFlagSet("simulspeak", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	ignoredConfigPath := ""
	fs.StringVar(&ignoredConfigPath, "config", "", "config file path")
	fs.StringVar(&cfg.Service.Name, "service-name", cfg.Service.Name, "service name")
	fs.StringVar(&cfg.HTTP.Address, "http-address", cfg.HTTP.Address, "HTTP listen address")
	fs.StringVar(&cfg.GRPC.Address, "grpc-address", cfg.GRPC.Address, "gRPC listen address")
	fs.StringVar(&cfg.JWT.Secret, "jwt-secret", cfg.JWT.Secret, "JWT secret")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	return nil
}

// configPathFromArgs 从命令行参数中提取 --config 的值。
func configPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return ""
}

// lookupEnv 查找 {prefix}_{name} 环境变量。
func lookupEnv(prefix, name string) (string, bool) {
	return os.LookupEnv(prefix + "_" + name)
}

func lookupAnyEnv(prefix string, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := lookupEnv(prefix, name); ok {
			return value, true
		}
	}
	return "", false
}

// splitCSV 按逗号分割字符串并去除空白项。
func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parsePositiveInt 把环境变量中的正整数转换为 int，非法值交由调用方忽略。
func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("value must be positive: %d", parsed)
	}
	return parsed, nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %q", value)
	}
}

// resolveSecret 从环境变量名或文件路径中读取密钥值写入 target。
func resolveSecret(target *string, envName, filePath string) error {
	if envName != "" {
		value, ok := os.LookupEnv(envName)
		if ok {
			*target = value
		}
	}

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		*target = strings.TrimSpace(string(data))
	}

	return nil
}
