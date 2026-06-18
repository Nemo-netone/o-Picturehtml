//  服务启动引导：App生命周期+Run(Start→ 信号监听→ Shutdown)
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrMissingContext = errors.New("bootstrap context is nil")
	ErrMissingStart   = errors.New("bootstrap start function is required")
)

type App struct {
	Name            string
	Start           func(context.Context) error
	Shutdown        func(context.Context) error
	ShutdownTimeout time.Duration
}

// Run 执行应用生命周期：启动 → 等待信号 → 优雅关闭。
func Run(ctx context.Context, app App) error {
	if ctx == nil {
		return ErrMissingContext
	}
	if app.Name == "" {
		app.Name = "app"
	}
	if app.Start == nil {
		return fmt.Errorf("%s: %w", app.Name, ErrMissingStart)
	}

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start %s: %w", app.Name, err)
	}

	<-ctx.Done()

	if app.Shutdown == nil {
		return nil
	}

	timeout := app.ShutdownTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := app.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown %s: %w", app.Name, err)
	}

	return nil
}

