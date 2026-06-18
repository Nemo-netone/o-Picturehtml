//  服务启动引导：配置加载+YAML/.env解析+生命周期管理(Start/Shutdown)
package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/bootstrap"
)

// 作用: 验证 Test Bootstrap_ Graceful Shutdown 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestBootstrap_GracefulShutdown(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	// 作用: 后台运行 bootstrap 流程，便于测试取消后的优雅退出。
	go func() {
		errCh <- bootstrap.Run(ctx, bootstrap.App{
			Name: "test-service",
			// 作用: 标记测试服务已启动。
			Start: func(ctx context.Context) error {
				close(started)
				return nil
			},
			// 作用: 标记测试服务已停止。
			Shutdown: func(ctx context.Context) error {
				close(stopped)
				return nil
			},
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("service did not start")
	}

	cancel()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("service did not shut down")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

