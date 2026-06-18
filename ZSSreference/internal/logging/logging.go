//  结构化日志：JSON格式+slog封装
package logging

import (
	"io"
	"log/slog"
	"os"
)

// NewJSONLogger 创建 JSON 格式的结构化日志器。
func NewJSONLogger(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}

	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

