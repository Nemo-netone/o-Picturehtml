//  事件总线：发布/订阅模式+Watch Hub
package eventbus_test

import (
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/eventbus"
)

// 作用: 验证 Test Watch Hub_ Subscribe Receive Events 场景的行为。
func TestWatchHub_SubscribeReceiveEvents(t *testing.T) {
	hub := eventbus.NewWatchHub()
	ch := hub.Subscribe("nodes")
	hub.Publish("nodes", eventbus.WatchEvent{Key: "k1", Type: eventbus.WatchEventPut})

	event := mustWatchEvent(t, ch)
	if event.Key != "k1" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

// 作用: 验证 Test Watch Hub_ Unsubscribe No Events 场景的行为。
func TestWatchHub_UnsubscribeNoEvents(t *testing.T) {
	hub := eventbus.NewWatchHub()
	ch := hub.Subscribe("nodes")
	hub.Unsubscribe("nodes", ch)
	hub.Publish("nodes", eventbus.WatchEvent{Key: "k1"})

	select {
	case event := <-ch:
		t.Fatalf("unexpected event after unsubscribe: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

// 作用: 验证 Test Watch Hub_ Multi Subscriber 场景的行为。
func TestWatchHub_MultiSubscriber(t *testing.T) {
	hub := eventbus.NewWatchHub()
	a := hub.Subscribe("nodes")
	b := hub.Subscribe("nodes")
	hub.Publish("nodes", eventbus.WatchEvent{Key: "k1"})

	_ = mustWatchEvent(t, a)
	_ = mustWatchEvent(t, b)
}

// 作用: 读取测试所需的 must Watch Event，失败时终止测试。
func mustWatchEvent(t *testing.T, ch <-chan eventbus.WatchEvent) eventbus.WatchEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
	return eventbus.WatchEvent{}
}

