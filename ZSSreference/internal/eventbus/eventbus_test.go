//  事件总线：发布/订阅模式+Watch Hub
package eventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test Event Bus_ In Memory Publish Subscribe 场景的行为。
func TestEventBus_InMemoryPublishSubscribe(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	ch := bus.Subscribe("calls")

	if err := bus.Publish(context.Background(), "calls", model.DomainEvent{ID: "evt-1", Type: model.EventTypeCallChanged}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	event := mustEvent(t, ch)
	if event.ID != "evt-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

// 作用: 验证 Test Event_ Persistence Policy_ Must Save 场景的行为。
func TestEvent_PersistencePolicy_MustSave(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	event := model.DomainEvent{ID: "evt-1", CallID: "call-1", Type: model.EventTypeCallChanged}
	if err := bus.PublishWithPolicy(context.Background(), "calls", event, eventbus.PersistenceMustSave); err != nil {
		t.Fatalf("publish with policy: %v", err)
	}
	replay := bus.ReplayByCallID("call-1")
	if len(replay) != 1 || replay[0].ID != "evt-1" {
		t.Fatalf("unexpected replay: %#v", replay)
	}
}

// 作用: 验证 Test Repository_ Tenant Isolation 场景的行为。
func TestRepository_TenantIsolation(t *testing.T) {
	repo := eventbus.NewMemoryLedger()
	_ = repo.SaveCallRecord(model.CallSession{ID: "call-1", TenantID: "tenant-a"})

	if _, err := repo.GetCallRecord("tenant-b", "call-1"); err == nil {
		t.Fatalf("expected cross tenant access to fail")
	}
}

// 作用: 验证 Test Repository_ Call Record C R U D 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestRepository_CallRecordCRUD(t *testing.T) {
	repo := eventbus.NewMemoryLedger()
	call := model.CallSession{ID: "call-1", TenantID: "tenant-a", Caller: "1001", Callee: "1002"}
	if err := repo.SaveCallRecord(call); err != nil {
		t.Fatalf("save call: %v", err)
	}
	got, err := repo.GetCallRecord("tenant-a", "call-1")
	if err != nil {
		t.Fatalf("get call: %v", err)
	}
	if got.Caller != "1001" {
		t.Fatalf("unexpected call: %#v", got)
	}
}

// 作用: 验证 Test Repository_ Transcription C R U D 场景的行为。
func TestRepository_TranscriptionCRUD(t *testing.T) {
	repo := eventbus.NewMemoryLedger()
	tr := model.ASRResult{CallID: "call-1", UtteranceID: "utt-1", Text: "hello", IsFinal: true}
	if err := repo.SaveTranscription("tenant-a", tr); err != nil {
		t.Fatalf("save transcription: %v", err)
	}
	got := repo.ListTranscriptions("tenant-a", "call-1")
	if len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("unexpected transcriptions: %#v", got)
	}
}

// 作用: 验证 Test Event Replay_ By Call I D 场景的行为。
func TestEventReplay_ByCallID(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	_ = bus.PublishWithPolicy(context.Background(), "ai", model.DomainEvent{ID: "evt-1", CallID: "call-1", Type: model.EventTypeAI}, eventbus.PersistenceMustSave)
	_ = bus.PublishWithPolicy(context.Background(), "ai", model.DomainEvent{ID: "evt-2", CallID: "call-2", Type: model.EventTypeAI}, eventbus.PersistenceMustSave)

	replay := bus.ReplayByCallID("call-1")
	if len(replay) != 1 || replay[0].ID != "evt-1" {
		t.Fatalf("unexpected replay: %#v", replay)
	}
}

func TestMemoryBus_PublishRequiredDelivers(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	ch, unsubscribe := bus.SubscribeWithOptions("pbx", eventbus.SubscribeOptions{Buffer: 1})
	defer unsubscribe()

	if err := bus.PublishRequired(context.Background(), "pbx", model.DomainEvent{ID: "evt-required", Type: model.EventTypeMedia}); err != nil {
		t.Fatalf("publish required: %v", err)
	}
	event := mustEvent(t, ch)
	if event.ID != "evt-required" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestMemoryBus_PublishRequiredBlockedSubscriberTimesOut(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	_, unsubscribe := bus.SubscribeWithOptions("pbx", eventbus.SubscribeOptions{Buffer: 0})
	defer unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := bus.PublishRequired(ctx, "pbx", model.DomainEvent{ID: "evt-blocked", Type: model.EventTypeMedia}); err == nil {
		t.Fatal("expected blocked required publish to fail")
	}
}

func TestMemoryBus_PublishBestEffortDropsWhenFull(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	ch, unsubscribe := bus.SubscribeWithOptions("pbx", eventbus.SubscribeOptions{Buffer: 1})
	defer unsubscribe()

	if err := bus.PublishBestEffort(context.Background(), "pbx", model.DomainEvent{ID: "evt-1", Type: model.EventTypeMedia}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := bus.PublishBestEffort(ctx, "pbx", model.DomainEvent{ID: "evt-2", Type: model.EventTypeMedia}); err != nil {
		t.Fatalf("best effort should not block: %v", err)
	}
	event := mustEvent(t, ch)
	if event.ID != "evt-1" {
		t.Fatalf("unexpected first event: %#v", event)
	}
	select {
	case event := <-ch:
		t.Fatalf("best effort should drop when full, got %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMemoryBus_UnsubscribeStopsDelivery(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	ch, unsubscribe := bus.SubscribeWithOptions("pbx", eventbus.SubscribeOptions{Buffer: 1})
	unsubscribe()

	if err := bus.PublishBestEffort(context.Background(), "pbx", model.DomainEvent{ID: "evt-after-unsubscribe", Type: model.EventTypeMedia}); err != nil {
		t.Fatalf("publish after unsubscribe: %v", err)
	}
	select {
	case event := <-ch:
		t.Fatalf("unsubscribed channel should not receive event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

// 作用: 读取测试所需的 must Event，失败时终止测试。
func mustEvent(t *testing.T, ch <-chan model.DomainEvent) model.DomainEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	return model.DomainEvent{}
}

