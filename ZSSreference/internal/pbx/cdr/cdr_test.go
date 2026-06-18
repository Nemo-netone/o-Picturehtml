//  通话详单(CDR)：记录每次通话的起止时间、时长、ASR/TMT/TTS消耗
package cdr_test

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/cdr"
)

// 作用: 验证 Test C D R_ Call Ended_ Calculates Duration 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestCDR_CallEnded_CalculatesDuration(t *testing.T) {
	service := cdr.NewService()
	started := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	answered := started.Add(5 * time.Second)
	ended := answered.Add(75 * time.Second)

	if err := service.Create(context.Background(), cdr.Record{TenantID: "tenant-a", CallID: "call-1", Caller: "1001", Callee: "1002", StartedAt: started}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.MarkAnswered(context.Background(), "tenant-a", "call-1", answered); err != nil {
		t.Fatalf("answer: %v", err)
	}
	record, err := service.End(context.Background(), "tenant-a", "call-1", ended)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if record.Duration != 75*time.Second {
		t.Fatalf("unexpected duration: %s", record.Duration)
	}
	if record.Summary == "" {
		t.Fatalf("expected summary placeholder")
	}
}

