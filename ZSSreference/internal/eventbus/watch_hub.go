//  Watch Hub：节点变更事件监听与分发
package eventbus

import "sync"

type WatchEventType string

const (
	WatchEventPut    WatchEventType = "put"
	WatchEventDelete WatchEventType = "delete"
)

type WatchEvent struct {
	Type WatchEventType
	Key  string
}

type WatchHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan WatchEvent]struct{}
}

// NewWatchHub 创建通用 watch 发布/订阅中心。
func NewWatchHub() *WatchHub {
	return &WatchHub{subscribers: map[string]map[chan WatchEvent]struct{}{}}
}

// Subscribe 订阅某 topic 的 WatchEvent。
func (h *WatchHub) Subscribe(topic string) <-chan WatchEvent {
	ch := make(chan WatchEvent, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subscribers[topic] == nil {
		h.subscribers[topic] = map[chan WatchEvent]struct{}{}
	}
	h.subscribers[topic][ch] = struct{}{}
	return ch
}

// Unsubscribe 取消订阅。
func (h *WatchHub) Unsubscribe(topic string, ch <-chan WatchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscriber := range h.subscribers[topic] {
		if (<-chan WatchEvent)(subscriber) == ch {
			delete(h.subscribers[topic], subscriber)
			return
		}
	}
}

// Publish 向某 topic 所有订阅者推送事件。
func (h *WatchHub) Publish(topic string, event WatchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers[topic] {
		select {
		case ch <- event:
		default:
		}
	}
}

