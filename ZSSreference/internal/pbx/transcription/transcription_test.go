//  转录：ASR结果持久化与导出
package transcription_test

import (
	"context"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/pbx/transcription"
)

// 作用: 验证 Test Transcription_ Write Search By Call 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTranscription_WriteSearchByCall(t *testing.T) {
	service := transcription.NewService()
	result := model.ASRResult{CallID: "call-1", UtteranceID: "utt-1", Text: "hello caller", IsFinal: true}

	if err := service.Write(context.Background(), "tenant-a", result); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := service.Write(context.Background(), "tenant-b", model.ASRResult{CallID: "call-1", Text: "other tenant", IsFinal: true}); err != nil {
		t.Fatalf("write other tenant: %v", err)
	}

	byCall, err := service.SearchByCall(context.Background(), "tenant-a", "call-1")
	if err != nil {
		t.Fatalf("search by call: %v", err)
	}
	if len(byCall) != 1 || byCall[0].Text != "hello caller" {
		t.Fatalf("unexpected call results: %#v", byCall)
	}
	byText, err := service.SearchText(context.Background(), "tenant-a", "caller")
	if err != nil {
		t.Fatalf("search text: %v", err)
	}
	if len(byText) != 1 || byText[0].CallID != "call-1" {
		t.Fatalf("unexpected text results: %#v", byText)
	}
}

