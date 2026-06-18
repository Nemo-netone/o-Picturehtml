// PBX存储层
package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
)

// 作用: 验证 Test Storage_ Put Get Presign 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestStorage_PutGetPresign(t *testing.T) {
	store := storage.NewMemoryStorage()

	object, err := store.Put(context.Background(), "recordings/call-1.wav", []byte("audio"), storage.PutOptions{ContentType: "audio/wav"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if object.Size != 5 || object.Checksum == "" {
		t.Fatalf("unexpected object metadata: %#v", object)
	}

	got, err := store.Get(context.Background(), object.Key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Data) != "audio" {
		t.Fatalf("unexpected data: %q", got.Data)
	}

	url, err := store.PresignGet(context.Background(), object.Key, time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.HasPrefix(url, "memory://recordings/call-1.wav?expires=") {
		t.Fatalf("unexpected presigned url: %s", url)
	}
}

func TestLocalStorage_PutGetDelete(t *testing.T) {
	store := storage.NewLocalStorage(t.TempDir())

	object, err := store.Put(context.Background(), "recordings/tenant-a/call-1/rec-1.wav", []byte("audio"), storage.PutOptions{ContentType: "audio/wav"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if object.Size != 5 || object.Checksum == "" {
		t.Fatalf("unexpected object metadata: %#v", object)
	}

	got, err := store.Get(context.Background(), object.Key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Data) != "audio" {
		t.Fatalf("unexpected data: %q", got.Data)
	}

	url, err := store.PresignGet(context.Background(), object.Key, time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.HasPrefix(url, "file://") || !strings.Contains(url, "expires=") {
		t.Fatalf("unexpected presigned url: %s", url)
	}

	if err := store.Delete(context.Background(), object.Key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(context.Background(), object.Key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLocalStorage_RejectsPathEscape(t *testing.T) {
	store := storage.NewLocalStorage(t.TempDir())
	if _, err := store.Put(context.Background(), "../escape.wav", []byte("audio"), storage.PutOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected path escape to be rejected, got %v", err)
	}
}
