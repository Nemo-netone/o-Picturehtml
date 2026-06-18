//  seed-demo 演示数据初始化：创建内存态注册中心和配置中心用于本地演示
package main

import (
	"log/slog"
	"os"

	"github.com/SATA260/SimulSpeak1/internal/configcenter"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/logging"
	"github.com/SATA260/SimulSpeak1/internal/registry"
)

// main 初始化一套内存态的 Demo 控制面（注册中心 + 配置中心），便于本地演示。
func main() {
	logger := logging.NewJSONLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)

	client := etcdutil.NewMemoryClient()
	reg := registry.New(client, registry.Options{})
	cfg := configcenter.New(client)
	_ = reg
	_ = cfg

	logger.Info("Demo 控制面已就绪：注册中心与配置中心已初始化（内存态）")
}

