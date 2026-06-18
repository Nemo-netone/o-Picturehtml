//  公共错误类型定义
package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	pbxerrors "github.com/SATA260/SimulSpeak1/internal/errors"
)

// 作用: 验证 Test Errors_ Sentinel Wrapping 场景的行为。
func TestErrors_SentinelWrapping(t *testing.T) {
	wrapped := fmt.Errorf("route call: %w", pbxerrors.ErrNoAvailableNode)

	if !stderrors.Is(wrapped, pbxerrors.ErrNoAvailableNode) {
		t.Fatalf("expected wrapped error to match ErrNoAvailableNode")
	}

	if stderrors.Is(wrapped, pbxerrors.ErrEpochMismatch) {
		t.Fatalf("did not expect wrapped error to match ErrEpochMismatch")
	}
}

