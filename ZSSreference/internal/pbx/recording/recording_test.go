// 录音：音频流录制与存储
package recording_test

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/recording"
	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
)

// 作用: 验证 Test Recording_ Metadata Write 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRecording_MetadataWrite(t *testing.T) {
	service := recording.NewService(storage.NewMemoryStorage())
	started := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)

	metadata, err := service.Start(context.Background(), "tenant-a", "call-1", "rec-1", started)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	metadata, err = service.CompleteUpload(context.Background(), "tenant-a", "rec-1", []byte("audio"), storage.SHA256([]byte("audio")), started.Add(time.Minute))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if metadata.ObjectKey != "recordings/tenant-a/call-1/rec-1.wav" || metadata.Size != 5 || metadata.Checksum == "" || metadata.Active {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

// 作用: 验证 Test Recording_ Checksum Validation 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRecording_ChecksumValidation(t *testing.T) {
	service := recording.NewService(storage.NewMemoryStorage())
	_, _ = service.Start(context.Background(), "tenant-a", "call-1", "rec-1", time.Now())

	_, err := service.CompleteUpload(context.Background(), "tenant-a", "rec-1", []byte("audio"), "bad-checksum", time.Now())
	if !errors.Is(err, storage.ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

// 作用: 验证 Test Retention_ Delete Expired 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRetention_DeleteExpired(t *testing.T) {
	store := storage.NewMemoryStorage()
	service := recording.NewService(store)
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	_, _ = service.Start(context.Background(), "tenant-a", "call-1", "rec-1", now.Add(-10*24*time.Hour))
	_, _ = service.CompleteUpload(context.Background(), "tenant-a", "rec-1", []byte("audio"), storage.SHA256([]byte("audio")), now.Add(-9*24*time.Hour))

	deleted, err := service.ApplyRetention(context.Background(), now, recording.RetentionPolicy{TenantID: "tenant-a", RetentionDays: 7})
	if err != nil {
		t.Fatalf("retention: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "rec-1" {
		t.Fatalf("unexpected deleted ids: %#v", deleted)
	}
	if _, err := service.Get(context.Background(), "tenant-a", "rec-1"); !errors.Is(err, recording.ErrNotFound) {
		t.Fatalf("expected metadata deleted, got %v", err)
	}
}

// 作用: 验证 Test Retention_ Legal Hold Skip 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRetention_LegalHoldSkip(t *testing.T) {
	service := recording.NewService(storage.NewMemoryStorage())
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	_, _ = service.Start(context.Background(), "tenant-a", "call-1", "rec-1", now.Add(-10*24*time.Hour))
	_, _ = service.CompleteUpload(context.Background(), "tenant-a", "rec-1", []byte("audio"), storage.SHA256([]byte("audio")), now.Add(-9*24*time.Hour))
	if err := service.SetLegalHold("tenant-a", "rec-1", true); err != nil {
		t.Fatalf("legal hold: %v", err)
	}

	deleted, err := service.ApplyRetention(context.Background(), now, recording.RetentionPolicy{TenantID: "tenant-a", RetentionDays: 7})
	if err != nil {
		t.Fatalf("retention: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("legal hold should skip delete: %#v", deleted)
	}
	if _, err := service.Get(context.Background(), "tenant-a", "rec-1"); err != nil {
		t.Fatalf("expected metadata retained: %v", err)
	}
}

// 作用: 验证 Test Recording_ Tenant Access Denied 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRecording_TenantAccessDenied(t *testing.T) {
	service := recording.NewService(storage.NewMemoryStorage())
	_, _ = service.Start(context.Background(), "tenant-a", "call-1", "rec-1", time.Now())

	_, err := service.Get(context.Background(), "tenant-b", "rec-1")
	if !errors.Is(err, recording.ErrAccessDenied) {
		t.Fatalf("expected access denied, got %v", err)
	}
	audit := service.AuditLog()
	if len(audit) != 1 || audit[0].Outcome != "denied" {
		t.Fatalf("expected denied audit, got %#v", audit)
	}
}

func TestPCM16WAVRecorder_UploadsWAV(t *testing.T) {
	store := storage.NewMemoryStorage()
	service := recording.NewService(store)
	recorder, started, err := recording.StartPCM16WAVRecorder(context.Background(), service, recording.PCM16WAVConfig{
		TenantID:    "tenant-a",
		CallID:      "call-1",
		RecordingID: "rec-1",
		SampleRate:  16000,
		Buffer:      2,
	})
	if err != nil {
		t.Fatalf("start recorder: %v", err)
	}
	if started.ObjectKey != "recordings/tenant-a/call-1/rec-1.wav" {
		t.Fatalf("unexpected object key: %s", started.ObjectKey)
	}
	if ok := recorder.WritePCM16LE([]byte{0x01, 0x00, 0x02, 0x00}); !ok {
		t.Fatal("expected recorder write to succeed")
	}
	metadata, err := recorder.Close(context.Background())
	if err != nil {
		t.Fatalf("close recorder: %v", err)
	}
	if metadata.Size != 48 || metadata.Checksum == "" || metadata.Active {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	object, err := store.Get(context.Background(), metadata.ObjectKey)
	if err != nil {
		t.Fatalf("get wav: %v", err)
	}
	if string(object.Data[:4]) != "RIFF" || string(object.Data[8:12]) != "WAVE" || string(object.Data[36:40]) != "data" {
		t.Fatalf("unexpected wav header: %q", object.Data[:44])
	}
	if sampleRate := binary.LittleEndian.Uint32(object.Data[24:28]); sampleRate != 16000 {
		t.Fatalf("unexpected wav sample rate: %d", sampleRate)
	}
	if dataSize := binary.LittleEndian.Uint32(object.Data[40:44]); dataSize != 4 {
		t.Fatalf("unexpected wav data size: %d", dataSize)
	}
}
