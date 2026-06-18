//  结构化日志：JSON格式+slog封装
package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/logging"
)

// 作用: 验证 Test Logger_ J S O N Fields 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestLogger_JSONFields(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewJSONLogger(&buf, slog.LevelInfo)

	logger.InfoContext(context.Background(), "通话已路由",
		slog.String("requestId", "req-1"),
		slog.String("tenantId", "tenant-1"),
		slog.String("callId", "call-1"),
	)

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON log output: %v", err)
	}

	if payload["msg"] != "通话已路由" {
		t.Fatalf("unexpected msg: %v", payload["msg"])
	}
	if payload["requestId"] != "req-1" {
		t.Fatalf("unexpected requestId: %v", payload["requestId"])
	}
	if payload["tenantId"] != "tenant-1" {
		t.Fatalf("unexpected tenantId: %v", payload["tenantId"])
	}
	if payload["callId"] != "call-1" {
		t.Fatalf("unexpected callId: %v", payload["callId"])
	}
}

