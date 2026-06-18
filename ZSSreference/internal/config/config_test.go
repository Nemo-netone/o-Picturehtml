// 应用配置：YAML+环境变量+命令行参数三合一加载
package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/config"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test Config_ Load Defaults 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfig_LoadDefaults(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Service.Name != "simulspeak" {
		t.Fatalf("unexpected service name: %s", cfg.Service.Name)
	}
	if cfg.HTTP.Address != "0.0.0.0:8080" {
		t.Fatalf("unexpected http address: %s", cfg.HTTP.Address)
	}
	if cfg.PBX.ControlAddress != "0.0.0.0:8081" || cfg.PBX.NodeWSURL != "ws://127.0.0.1:8081/pbx/ws" {
		t.Fatalf("unexpected pbx control defaults: %#v", cfg.PBX)
	}
	if cfg.PBX.WebRTCUDPPortMin != 20000 || cfg.PBX.WebRTCUDPPortMax != 20100 {
		t.Fatalf("unexpected pbx webrtc udp defaults: %#v", cfg.PBX)
	}
	if len(cfg.Etcd.Endpoints) != 1 || cfg.Etcd.Endpoints[0] != "http://127.0.0.1:2379" {
		t.Fatalf("unexpected etcd endpoints: %#v", cfg.Etcd.Endpoints)
	}
	if cfg.Etcd.Mode != "memory" {
		t.Fatalf("unexpected etcd mode: %s", cfg.Etcd.Mode)
	}
	if cfg.Node.Advertise != "ws://127.0.0.1:8081/pbx/ws" || cfg.Node.MaxCalls != 100 || cfg.Node.Weight != 1 {
		t.Fatalf("unexpected node defaults: %#v", cfg.Node)
	}
	if cfg.Recording.Enabled || cfg.Recording.Directory != "./data/recordings" || cfg.Recording.SampleRate != 16000 {
		t.Fatalf("unexpected recording defaults: %#v", cfg.Recording)
	}
}

// 作用: 验证 Test Config_ Load Y A M L 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfig_LoadYAML(t *testing.T) {
	path := writeTempConfig(t, `
service:
  name: api-server
http:
  address: 127.0.0.1:8080
etcd:
  endpoints:
    - http://etcd:2379
jwt:
  issuer: simulspeak-test
  secret: yaml-secret
features:
  global:
    realtimeAI: true
`)

	cfg, err := config.Load(config.LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}

	if cfg.Service.Name != "api-server" {
		t.Fatalf("unexpected service name: %s", cfg.Service.Name)
	}
	if cfg.HTTP.Address != "127.0.0.1:8080" {
		t.Fatalf("unexpected http address: %s", cfg.HTTP.Address)
	}
	if cfg.JWT.Secret != "yaml-secret" {
		t.Fatalf("expected yaml secret to load")
	}
	if !cfg.Features.Enabled("tenant-a", "realtimeAI") {
		t.Fatalf("expected realtimeAI global flag to be enabled")
	}
}

// 作用: 验证 Test Config_ Env Override 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfig_EnvOverride(t *testing.T) {
	path := writeTempConfig(t, `
service:
  name: api-server
etcd:
  endpoints:
    - http://yaml-etcd:2379
jwt:
  secret: yaml-secret
`)

	t.Setenv("SIMULSPEAK_SERVICE_NAME", "signaling-gateway")
	t.Setenv("SIMULSPEAK_ETCD_MODE", "etcd")
	t.Setenv("SIMULSPEAK_ETCD_ENDPOINTS", "http://etcd-a:2379,http://etcd-b:2379")
	t.Setenv("SIMULSPEAK_JWT_SECRET", "env-secret")
	t.Setenv("SIMULSPEAK_PBX_CONTROL_ADDRESS", "127.0.0.1:28081")
	t.Setenv("SIMULSPEAK_PBX_NODE_WS_URL", "ws://127.0.0.1:28081/pbx/ws")
	t.Setenv("SIMULSPEAK_PBX_WEBRTC_UDP_PORT_MIN", "21000")
	t.Setenv("SIMULSPEAK_PBX_WEBRTC_UDP_PORT_MAX", "21100")
	t.Setenv("SIMULSPEAK_NODE_ID", "media-env")
	t.Setenv("SIMULSPEAK_NODE_ADVERTISE", "ws://10.0.0.8:28081/pbx/ws")
	t.Setenv("SIMULSPEAK_NODE_ZONE", "az-b")
	t.Setenv("SIMULSPEAK_NODE_MAX_CALLS", "12")
	t.Setenv("SIMULSPEAK_NODE_WEIGHT", "3")
	t.Setenv("SIMULSPEAK_SQLITE_DSN", "file:/tmp/simulspeak-test.db?_pragma=journal_mode(WAL)")
	t.Setenv("SIMULSPEAK_RECORDING_ENABLED", "yes")
	t.Setenv("SIMULSPEAK_RECORDING_DIR", "/tmp/simulspeak-recordings")
	t.Setenv("SIMULSPEAK_RECORDING_SAMPLE_RATE", "8000")
	t.Setenv("SIMULSPEAK_VAD_PROVIDER", "silero")
	t.Setenv("SIMULSPEAK_VAD_MODEL_PATH", "/opt/models/silero-vad.onnx")
	t.Setenv("SIMULSPEAK_ONNX_RUNTIME_LIBRARY_PATH", "/usr/local/lib/libonnxruntime.so")
	t.Setenv("SIMULSPEAK_VAD_SAMPLE_RATE", "8000")
	t.Setenv("SIMULSPEAK_ASR_PROVIDER", "tencent-asr")
	t.Setenv("SIMULSPEAK_TMT_PROVIDER", "tencent-tmt")
	t.Setenv("SIMULSPEAK_TTS_PROVIDER", "tencent-tts")
	t.Setenv("SIMULSPEAK_ASR_API_KEY", "legacy-asr-api-key")
	t.Setenv("SIMULSPEAK_TTS_API_KEY", "legacy-tts-api-key")
	t.Setenv("SIMULSPEAK_TENCENT_ASR_ENDPOINT", "wss://asr.example.com/asr/v2")
	t.Setenv("SIMULSPEAK_TENCENT_ASR_APPID", "1250000001")
	t.Setenv("SIMULSPEAK_TENCENT_ASR_SECRETID", "asr-secret-id")
	t.Setenv("SIMULSPEAK_TENCENT_ASR_SECRETKEY", "asr-secret-key")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_ENDPOINT", "https://tmt.example.com")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_REGION", "ap-shanghai")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_PROJECT_ID", "42")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_SECRETID", "tmt-secret-id")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_SECRETKEY", "tmt-secret-key")
	t.Setenv("SIMULSPEAK_TENCENT_TTS_ENDPOINT", "https://tts.example.com")
	t.Setenv("SIMULSPEAK_TENCENT_TTS_APPID", "1250000002")
	t.Setenv("SIMULSPEAK_TENCENT_TTS_SECRETID", "tts-secret-id")
	t.Setenv("SIMULSPEAK_TENCENT_TTS_SECRETKEY", "tts-secret-key")
	t.Setenv("SIMULSPEAK_LLM_PROVIDER", "openai-compatible")
	t.Setenv("SIMULSPEAK_LLM_ENDPOINT", "https://llm.example.com/v1")
	t.Setenv("SIMULSPEAK_LLM_API_KEY", "llm-api-key")
	t.Setenv("SIMULSPEAK_LLM_MODEL", "deepseek-chat")

	cfg, err := config.Load(config.LoadOptions{Path: path, EnvPrefix: "SIMULSPEAK"})
	if err != nil {
		t.Fatalf("load env override: %v", err)
	}

	if cfg.Service.Name != "signaling-gateway" {
		t.Fatalf("env service name did not override yaml: %s", cfg.Service.Name)
	}
	if strings.Join(cfg.Etcd.Endpoints, ",") != "http://etcd-a:2379,http://etcd-b:2379" {
		t.Fatalf("env etcd endpoints did not override yaml: %#v", cfg.Etcd.Endpoints)
	}
	if cfg.Etcd.Mode != "etcd" {
		t.Fatalf("env etcd mode did not override default: %s", cfg.Etcd.Mode)
	}
	if cfg.JWT.Secret != "env-secret" {
		t.Fatalf("env secret did not override yaml")
	}
	if cfg.PBX.ControlAddress != "127.0.0.1:28081" || cfg.PBX.NodeWSURL != "ws://127.0.0.1:28081/pbx/ws" {
		t.Fatalf("env pbx control config did not override defaults: %#v", cfg.PBX)
	}
	if cfg.PBX.WebRTCUDPPortMin != 21000 || cfg.PBX.WebRTCUDPPortMax != 21100 {
		t.Fatalf("env pbx webrtc udp config did not override defaults: %#v", cfg.PBX)
	}
	if cfg.Node.ID != "media-env" || cfg.Node.Advertise != "ws://10.0.0.8:28081/pbx/ws" || cfg.Node.Zone != "az-b" || cfg.Node.MaxCalls != 12 || cfg.Node.Weight != 3 {
		t.Fatalf("env node config did not override defaults: %#v", cfg.Node)
	}
	if cfg.Database.SQLite.DSN != "file:/tmp/simulspeak-test.db?_pragma=journal_mode(WAL)" {
		t.Fatalf("env sqlite dsn did not override defaults: %s", cfg.Database.SQLite.DSN)
	}
	if !cfg.Recording.Enabled || cfg.Recording.Directory != "/tmp/simulspeak-recordings" || cfg.Recording.SampleRate != 8000 {
		t.Fatalf("env recording config did not override defaults: %#v", cfg.Recording)
	}
	if cfg.AI.VAD.Provider != "silero" {
		t.Fatalf("env vad provider did not override defaults: %s", cfg.AI.VAD.Provider)
	}
	if cfg.AI.VAD.ModelPath != "/opt/models/silero-vad.onnx" {
		t.Fatalf("env vad model path did not override defaults: %s", cfg.AI.VAD.ModelPath)
	}
	if cfg.AI.VAD.RuntimeLibraryPath != "/usr/local/lib/libonnxruntime.so" {
		t.Fatalf("env onnx runtime path did not override defaults: %s", cfg.AI.VAD.RuntimeLibraryPath)
	}
	if cfg.AI.VAD.SampleRate != 8000 {
		t.Fatalf("env vad sample rate did not override defaults: %d", cfg.AI.VAD.SampleRate)
	}
	if cfg.AI.ASR.Provider != "tencent-asr" || cfg.AI.TMT.Provider != "tencent-tmt" || cfg.AI.TTS.Provider != "tencent-tts" {
		t.Fatalf("env provider override failed: asr=%s tmt=%s tts=%s", cfg.AI.ASR.Provider, cfg.AI.TMT.Provider, cfg.AI.TTS.Provider)
	}
	if cfg.AI.ASR.APIKey != "" {
		t.Fatalf("legacy asr api key env should be ignored: %s", cfg.AI.ASR.APIKey)
	}
	if cfg.AI.TTS.APIKey != "" {
		t.Fatalf("legacy tts api key env should be ignored: %s", cfg.AI.TTS.APIKey)
	}
	if cfg.AI.ASR.Endpoint != "wss://asr.example.com/asr/v2" || cfg.AI.TTS.Endpoint != "https://tts.example.com" {
		t.Fatalf("provider-specific endpoint did not apply: asr=%s tts=%s", cfg.AI.ASR.Endpoint, cfg.AI.TTS.Endpoint)
	}
	if cfg.AI.ASR.AppID != "1250000001" || cfg.AI.TTS.AppID != "1250000002" {
		t.Fatalf("provider-specific tencent appid did not apply: asr=%s tts=%s", cfg.AI.ASR.AppID, cfg.AI.TTS.AppID)
	}
	if cfg.AI.ASR.SecretID != "asr-secret-id" || cfg.AI.ASR.SecretKey != "asr-secret-key" {
		t.Fatalf("provider-specific asr secrets did not apply")
	}
	if cfg.AI.TMT.Endpoint != "https://tmt.example.com" || cfg.AI.TMT.Params["region"] != "ap-shanghai" || cfg.AI.TMT.Params["projectId"] != "42" {
		t.Fatalf("provider-specific tmt config did not apply: %#v", cfg.AI.TMT)
	}
	if cfg.AI.TMT.SecretID != "tmt-secret-id" || cfg.AI.TMT.SecretKey != "tmt-secret-key" {
		t.Fatalf("provider-specific tmt secrets did not apply")
	}
	if cfg.AI.TTS.SecretID != "tts-secret-id" || cfg.AI.TTS.SecretKey != "tts-secret-key" {
		t.Fatalf("provider-specific tts secrets did not apply")
	}
	if cfg.AI.LLM.Provider != "openai-compatible" || cfg.AI.LLM.Endpoint != "https://llm.example.com/v1" || cfg.AI.LLM.APIKey != "llm-api-key" || cfg.AI.LLM.Model != "deepseek-chat" {
		t.Fatalf("llm env config did not apply: %#v", cfg.AI.LLM)
	}
	providers := config.ProviderConfigsFromAIConfig(cfg.AI)
	if providers[model.CapabilityTypeASR].Params["appId"] != "1250000001" {
		t.Fatalf("asr provider config missing appId: %#v", providers[model.CapabilityTypeASR])
	}
	if providers[model.CapabilityTypeASR].Endpoint != "wss://asr.example.com/asr/v2" {
		t.Fatalf("asr provider config missing endpoint: %#v", providers[model.CapabilityTypeASR])
	}
	if providers[model.CapabilityTypeTTS].Params["appId"] != "1250000002" {
		t.Fatalf("tts provider config missing appId: %#v", providers[model.CapabilityTypeTTS])
	}
	if providers[model.CapabilityTypeTMT].Params["region"] != "ap-shanghai" || providers[model.CapabilityTypeTMT].Params["projectId"] != "42" {
		t.Fatalf("tmt provider config missing params: %#v", providers[model.CapabilityTypeTMT])
	}
	if providers[model.CapabilityTypeTMT].Secrets["secretKey"] != "tmt-secret-key" {
		t.Fatalf("tmt provider config missing secret: %#v", providers[model.CapabilityTypeTMT].Secrets)
	}
	if providers[model.CapabilityTypeTTS].Endpoint != "https://tts.example.com" {
		t.Fatalf("tts provider config missing endpoint: %#v", providers[model.CapabilityTypeTTS])
	}
	if providers[model.CapabilityTypeTTS].Secrets["secretKey"] != "tts-secret-key" {
		t.Fatalf("tts provider config missing secret: %#v", providers[model.CapabilityTypeTTS].Secrets)
	}
}

func TestConfig_TranslateProviderAliasFeedsTMT(t *testing.T) {
	t.Setenv("SIMULSPEAK_TRANSLATE_PROVIDER", "tencent-tmt")
	t.Setenv("SIMULSPEAK_TRANSLATE_ENDPOINT", "https://alias-tmt.example.com")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_REGION", "ap-guangzhou")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_PROJECT_ID", "7")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_SECRETID", "alias-secret-id")
	t.Setenv("SIMULSPEAK_TENCENT_TMT_SECRETKEY", "alias-secret-key")

	cfg, err := config.Load(config.LoadOptions{EnvPrefix: "SIMULSPEAK"})
	if err != nil {
		t.Fatalf("load alias env: %v", err)
	}

	if cfg.AI.TMT.Provider != "tencent-tmt" {
		t.Fatalf("translate provider alias did not apply: %s", cfg.AI.TMT.Provider)
	}
	if cfg.AI.TMT.Endpoint != "https://alias-tmt.example.com" {
		t.Fatalf("translate endpoint alias did not apply: %s", cfg.AI.TMT.Endpoint)
	}
	if cfg.AI.TMT.Params["region"] != "ap-guangzhou" || cfg.AI.TMT.Params["projectId"] != "7" {
		t.Fatalf("tmt provider params did not apply: %#v", cfg.AI.TMT.Params)
	}
	if cfg.AI.TMT.SecretID != "alias-secret-id" || cfg.AI.TMT.SecretKey != "alias-secret-key" {
		t.Fatalf("tmt secrets did not apply")
	}
}

// 作用: 验证 Test Config_ Flag Override 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfig_FlagOverride(t *testing.T) {
	path := writeTempConfig(t, `
service:
  name: yaml-service
http:
  address: 127.0.0.1:8080
jwt:
  secret: yaml-secret
`)

	t.Setenv("SIMULSPEAK_SERVICE_NAME", "env-service")

	cfg, err := config.Load(config.LoadOptions{
		Path:      path,
		EnvPrefix: "SIMULSPEAK",
		Args: []string{
			"--service-name", "cli-service",
			"--http-address", "127.0.0.1:28080",
			"--jwt-secret", "cli-secret",
		},
	})
	if err != nil {
		t.Fatalf("load flag override: %v", err)
	}

	if cfg.Service.Name != "cli-service" {
		t.Fatalf("flag service name did not override env/yaml: %s", cfg.Service.Name)
	}
	if cfg.HTTP.Address != "127.0.0.1:28080" {
		t.Fatalf("flag http address did not override yaml: %s", cfg.HTTP.Address)
	}
	if cfg.JWT.Secret != "cli-secret" {
		t.Fatalf("flag jwt secret did not override yaml")
	}
}

// 作用: 验证 Test Config_ Required Secret Missing 场景的行为。
func TestConfig_RequiredSecretMissing(t *testing.T) {
	t.Setenv("SIMULSPEAK_JWT_SECRET", "")

	_, err := config.Load(config.LoadOptions{
		EnvPrefix:      "SIMULSPEAK",
		RequireSecrets: true,
	})
	if !errors.Is(err, config.ErrMissingSecret) {
		t.Fatalf("expected ErrMissingSecret, got %v", err)
	}
}

// 作用: 验证 Test Config_ Secret Redaction 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestConfig_SecretRedaction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.JWT.Secret = "super-secret"
	cfg.AI.ASR.APIKey = "asr-secret"
	cfg.AI.ASR.SecretID = "asr-secret-id"
	cfg.AI.ASR.SecretKey = "asr-secret-key"
	cfg.AI.TMT.SecretID = "tmt-secret-id"
	cfg.AI.TMT.SecretKey = "tmt-secret-key"
	cfg.AI.TTS.APIKey = "tts-secret"
	cfg.AI.TTS.SecretID = "tts-secret-id"
	cfg.AI.TTS.SecretKey = "tts-secret-key"
	cfg.AI.LLM.APIKey = "llm-secret"
	cfg.AI.LLM.SecretID = "llm-secret-id"
	cfg.AI.LLM.SecretKey = "llm-secret-key"

	redacted := cfg.Redacted()
	data, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}

	out := string(data)
	for _, secret := range []string{"super-secret", "asr-secret", "asr-secret-id", "asr-secret-key", "tmt-secret-id", "tmt-secret-key", "tts-secret", "tts-secret-id", "tts-secret-key", "llm-secret", "llm-secret-id", "llm-secret-key"} {
		if strings.Contains(out, secret) {
			t.Fatalf("redacted config leaked secret %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, config.RedactedValue) {
		t.Fatalf("expected redacted marker in output: %s", out)
	}
}

// 作用: 验证 Test Feature Flag_ Tenant Override 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestFeatureFlag_TenantOverride(t *testing.T) {
	flags := config.FeatureFlags{
		Global: map[string]bool{
			"bargeIn":   false,
			"recording": true,
		},
		Tenants: map[string]map[string]bool{
			"tenant-a": {
				"bargeIn": true,
			},
		},
	}

	if !flags.Enabled("tenant-a", "bargeIn") {
		t.Fatalf("expected tenant override to enable bargeIn")
	}
	if flags.Enabled("tenant-b", "bargeIn") {
		t.Fatalf("expected global bargeIn false for tenant-b")
	}
	if !flags.Enabled("tenant-b", "recording") {
		t.Fatalf("expected global recording true for tenant-b")
	}
}

// 作用: 验证 Test Config_ Load Env File 场景的行为。
// 逻辑: 写入 dotenv 文件，加载后断言变量进入环境并经 Load 生效。
func TestConfig_LoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := strings.Join([]string{
		"# 注释行应被忽略",
		"",
		"export SIMULSPEAK_SERVICE_NAME=env-file-svc",
		`SIMULSPEAK_HTTP_ADDRESS="127.0.0.1:29090"`,
		"SIMULSPEAK_ETCD_ENDPOINTS=http://a:2379,http://b:2379",
		"SIMULSPEAK_VAD_SAMPLE_RATE=8000",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	keys := []string{"SIMULSPEAK_SERVICE_NAME", "SIMULSPEAK_HTTP_ADDRESS", "SIMULSPEAK_ETCD_ENDPOINTS", "SIMULSPEAK_VAD_SAMPLE_RATE"}
	t.Cleanup(func() {
		for _, key := range keys {
			_ = os.Unsetenv(key)
		}
	})

	if err := config.LoadEnvFile(path); err != nil {
		t.Fatalf("load env file: %v", err)
	}
	if got := os.Getenv("SIMULSPEAK_HTTP_ADDRESS"); got != "127.0.0.1:29090" {
		t.Fatalf("quoted value not unquoted: %q", got)
	}

	cfg, err := config.Load(config.LoadOptions{EnvPrefix: "SIMULSPEAK"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Service.Name != "env-file-svc" {
		t.Fatalf("service name from env file: %s", cfg.Service.Name)
	}
	if cfg.HTTP.Address != "127.0.0.1:29090" {
		t.Fatalf("http address from env file: %s", cfg.HTTP.Address)
	}
	if len(cfg.Etcd.Endpoints) != 2 {
		t.Fatalf("etcd endpoints from env file: %#v", cfg.Etcd.Endpoints)
	}
	if cfg.AI.VAD.SampleRate != 8000 {
		t.Fatalf("vad sample rate from env file: %d", cfg.AI.VAD.SampleRate)
	}
}

// 作用: 验证 Test Config_ Load Env File_ Precedence And Missing 场景的行为。
// 逻辑: 已存在的环境变量不被 .env 覆盖；文件缺失时返回 nil。
func TestConfig_LoadEnvFile_PrecedenceAndMissing(t *testing.T) {
	t.Setenv("SIMULSPEAK_SERVICE_NAME", "real-env")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("SIMULSPEAK_SERVICE_NAME=from-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := config.LoadEnvFile(path); err != nil {
		t.Fatalf("load env file: %v", err)
	}
	if got := os.Getenv("SIMULSPEAK_SERVICE_NAME"); got != "real-env" {
		t.Fatalf("env file must not override existing env, got %q", got)
	}

	if err := config.LoadEnvFile(filepath.Join(dir, "nonexistent.env")); err != nil {
		t.Fatalf("missing env file should be nil, got %v", err)
	}
}

// 作用: 处理 write Temp Config 的核心流程。
func writeTempConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
