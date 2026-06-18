//  事件总线：发布/订阅模式实现
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

var ErrNotFound = errors.New("ledger record not found")
var ErrSubscriberBlocked = errors.New("eventbus subscriber blocked")

type PersistencePolicy string

const (
	PersistenceMustSave   PersistencePolicy = "must-save"
	PersistenceBestEffort PersistencePolicy = "best-effort"
	PersistenceEphemeral  PersistencePolicy = "ephemeral"
)

type MemoryBus struct {
	mu             sync.Mutex
	nextSubscriber int64
	subscribers    map[string][]memorySubscriber
	events         []model.DomainEvent
}

type memorySubscriber struct {
	id int64
	ch chan model.DomainEvent
}

type SubscribeOptions struct {
	Buffer int
}

// NewMemoryBus 创建内存事件总线。
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subscribers: map[string][]memorySubscriber{}, events: []model.DomainEvent{}}
}

// Subscribe 订阅某 topic 的事件流，返回带缓冲的 channel。
func (b *MemoryBus) Subscribe(topic string) <-chan model.DomainEvent {
	ch, _ := b.SubscribeWithOptions(topic, SubscribeOptions{Buffer: 16})
	return ch
}

// SubscribeWithOptions 订阅某 topic，支持自定义缓冲区和显式取消订阅。
func (b *MemoryBus) SubscribeWithOptions(topic string, options SubscribeOptions) (<-chan model.DomainEvent, func()) {
	buffer := options.Buffer
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan model.DomainEvent, buffer)
	b.mu.Lock()
	b.nextSubscriber++
	id := b.nextSubscriber
	b.subscribers[topic] = append(b.subscribers[topic], memorySubscriber{id: id, ch: ch})
	b.mu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.unsubscribe(topic, id)
		})
	}
	return ch, unsubscribe
}

// Publish 以 ephemeral 策略发布事件到某 topic。
func (b *MemoryBus) Publish(ctx context.Context, topic string, event model.DomainEvent) error {
	return b.PublishWithPolicy(ctx, topic, event, PersistenceEphemeral)
}

// PublishWithPolicy 按指定持久化策略发布事件：must-save/best-effort 时存入历史，所有订阅者收到推送。
func (b *MemoryBus) PublishWithPolicy(ctx context.Context, topic string, event model.DomainEvent, policy PersistencePolicy) error {
	return b.publish(ctx, topic, event, policy, false)
}

// PublishRequired 发布必须送达事件；订阅者阻塞时等待 context，超时返回错误。
func (b *MemoryBus) PublishRequired(ctx context.Context, topic string, event model.DomainEvent) error {
	return b.publish(ctx, topic, event, PersistenceEphemeral, true)
}

// PublishBestEffort 发布尽力而为事件；订阅者阻塞时直接丢弃。
func (b *MemoryBus) PublishBestEffort(ctx context.Context, topic string, event model.DomainEvent) error {
	return b.publish(ctx, topic, event, PersistenceEphemeral, false)
}

func (b *MemoryBus) publish(ctx context.Context, topic string, event model.DomainEvent, policy PersistencePolicy, required bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if policy == PersistenceMustSave || policy == PersistenceBestEffort {
		b.events = append(b.events, event)
	}
	subscribers := append([]memorySubscriber(nil), b.subscribers[topic]...)
	b.mu.Unlock()
	for _, subscriber := range subscribers {
		if required {
			select {
			case subscriber.ch <- event:
			case <-ctx.Done():
				return fmt.Errorf("%w: topic=%s subscriber=%d: %w", ErrSubscriberBlocked, topic, subscriber.id, ctx.Err())
			}
			continue
		}
		select {
		case subscriber.ch <- event:
		default:
		}
	}
	return nil
}

// ReplayByCallID 从持久化历史中重放某通话的所有事件。
func (b *MemoryBus) ReplayByCallID(callID string) []model.DomainEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]model.DomainEvent, 0)
	for _, event := range b.events {
		if event.CallID == callID {
			out = append(out, event)
		}
	}
	return out
}

func (b *MemoryBus) unsubscribe(topic string, id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subscribers := b.subscribers[topic]
	for index, subscriber := range subscribers {
		if subscriber.id == id {
			copy(subscribers[index:], subscribers[index+1:])
			subscribers = subscribers[:len(subscribers)-1]
			break
		}
	}
	if len(subscribers) == 0 {
		delete(b.subscribers, topic)
		return
	}
	b.subscribers[topic] = subscribers
}

type MemoryLedger struct {
	mu             sync.Mutex
	calls          map[string]model.CallSession
	transcriptions map[string][]model.ASRResult
}

// NewMemoryLedger 创建内存通话账本（存储通话记录和转写结果）。
func NewMemoryLedger() *MemoryLedger {
	return &MemoryLedger{
		calls:          map[string]model.CallSession{},
		transcriptions: map[string][]model.ASRResult{},
	}
}

// SaveCallRecord 保存通话记录到内存。
func (l *MemoryLedger) SaveCallRecord(call model.CallSession) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls[call.ID] = call
	return nil
}

// GetCallRecord 查询通话记录（校验租户归属）。
func (l *MemoryLedger) GetCallRecord(tenantID, callID string) (model.CallSession, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	call, ok := l.calls[callID]
	if !ok || call.TenantID != tenantID {
		return model.CallSession{}, ErrNotFound
	}
	return call, nil
}

// SaveTranscription 保存转写结果到内存。
func (l *MemoryLedger) SaveTranscription(tenantID string, result model.ASRResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := tenantID + ":" + result.CallID
	l.transcriptions[key] = append(l.transcriptions[key], result)
	return nil
}

// ListTranscriptions 列出某通话的所有转写结果。
func (l *MemoryLedger) ListTranscriptions(tenantID, callID string) []model.ASRResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := tenantID + ":" + callID
	return append([]model.ASRResult(nil), l.transcriptions[key]...)
}

