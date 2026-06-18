//  安全：鉴权+加密+令牌管理
package security_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
	"github.com/SATA260/SimulSpeak1/internal/security"
)

// 作用: 验证 Test Security_ Secret Redacted 场景的行为。
func TestSecurity_SecretRedacted(t *testing.T) {
	line := "provider request api_key=sk-live token=abc password=hunter2 ok=true"
	redacted := security.RedactSecrets(line)

	if strings.Contains(redacted, "sk-live") || strings.Contains(redacted, "hunter2") || strings.Contains(redacted, "abc") {
		t.Fatalf("secret leaked: %s", redacted)
	}
	values := security.RedactMap(map[string]string{"Authorization": "Bearer token", "tenantId": "tenant-a"})
	if values["Authorization"] != "[REDACTED]" || values["tenantId"] != "tenant-a" {
		t.Fatalf("unexpected redacted map: %#v", values)
	}
}

// 作用: 验证 Test Security_ S S R F Private I P Blocked 场景的行为。
func TestSecurity_SSRFPrivateIPBlocked(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1/admin",
		"http://10.0.0.1/admin",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	} {
		if err := security.ValidateProviderURL(target); !errors.Is(err, security.ErrSSRFBlocked) {
			t.Fatalf("expected ssrf block for %s, got %v", target, err)
		}
	}
	if err := security.ValidateProviderURL("https://api.example.com/v1"); err != nil {
		t.Fatalf("public provider should be allowed: %v", err)
	}
}

// 作用: 验证 Test Security_ Recording Audit Created 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestSecurity_RecordingAuditCreated(t *testing.T) {
	service := recording.NewService(storage.NewMemoryStorage())
	_, _ = service.Start(context.Background(), "tenant-a", "call-1", "rec-1", time.Now())

	_, err := service.Get(context.Background(), "tenant-b", "rec-1")
	if !errors.Is(err, recording.ErrAccessDenied) {
		t.Fatalf("expected access denied, got %v", err)
	}
	audit := service.AuditLog()
	if len(audit) != 1 || audit[0].Action != "get" || audit[0].Outcome != "denied" {
		t.Fatalf("expected denied recording audit, got %#v", audit)
	}
}

