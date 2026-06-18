//  配置加载：YAML+.env+命令行参数三合一→ AppConfig
package bootstrap

import (
	"os"

	"github.com/SATA260/SimulSpeak1/internal/config"
)

// LoadServiceConfig 加载配置并确保 service name 有默认值。
// 先加载 .env 文件（路径可用 SIMULSPEAK_ENV_FILE 覆盖，默认 ./.env，缺失则跳过），
// 再按 默认值 → YAML → 环境变量 → 命令行 的优先级链解析配置。
func LoadServiceConfig(defaultName string, args []string) (*config.AppConfig, error) {
	envFile := os.Getenv("SIMULSPEAK_ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	if err := config.LoadEnvFile(envFile); err != nil {
		return nil, err
	}

	cfg, err := config.Load(config.LoadOptions{
		EnvPrefix: "SIMULSPEAK",
		Args:      args,
	})
	if err != nil {
		return nil, err
	}

	if cfg.Service.Name == "" || cfg.Service.Name == "simulspeak" {
		cfg.Service.Name = defaultName
	}

	return cfg, nil
}

