//  媒体处理：Opus编解码+PCM重采样+音频帧管理
package media

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// ErrCallNotFound 表示指定通话在媒体适配器中不存在。
var ErrCallNotFound = errors.New("media call not found")

// CallState 表示媒体通话当前所处的生命周期状态。
type CallState string

const (
	// CallStateRinging 表示通话已创建但尚未桥接。
	CallStateRinging CallState = "ringing"
	// CallStateBridged 表示通话双方已桥接。
	CallStateBridged CallState = "bridged"
	// CallStateHungup 表示通话已挂断。
	CallStateHungup CallState = "hungup"
)

// EventType 表示从 PBX 原始事件归一化后的事件类型。
type EventType string

const (
	// EventCallCreated 表示 PBX 上报了通话创建事件。
	EventCallCreated EventType = "call_created"
	// EventCallAnswered 表示 PBX 上报了通话接听事件。
	EventCallAnswered EventType = "call_answered"
	// EventCallHangup 表示 PBX 上报了通话挂断事件。
	EventCallHangup EventType = "call_hangup"
	// EventUnknown 表示 PBX 原始事件暂未被系统识别。
	EventUnknown EventType = "unknown"
)

// OriginateRequest 描述发起外呼通话所需的基础号码信息。
type OriginateRequest struct {
	CallID string
	Caller string
	Callee string
}

// Call 描述媒体平面中一通电话的基础状态。
type Call struct {
	ID     string
	Caller string
	Callee string
	State  CallState
}

// Recording 描述通话录音文件及其启停状态。
type Recording struct {
	CallID    string
	ObjectKey string
	Active    bool
}

// AudioFrame 表示媒体旁路流中的一帧音频数据。
type AudioFrame struct {
	CallID  string
	Payload []byte
}

// AudioChunk 表示注入到通话中的一段音频数据。
type AudioChunk struct {
	CallID  string
	Payload []byte
}

// AudioStream 表示可输入和输出音频帧的双工媒体流。
type AudioStream struct {
	Input  chan AudioFrame
	Output chan AudioFrame
}

// Event 表示 PBX 事件归一化后的类型化事件。
type Event struct {
	Type   EventType
	CallID string
	Raw    string
}

// Controller 定义媒体平面通话控制和音频处理能力。
type Controller interface {
	// Originate 发起一路媒体通话并返回创建后的通话状态。
	Originate(ctx context.Context, req OriginateRequest) (Call, error)
	// Bridge 将指定通话切换为桥接状态。
	Bridge(ctx context.Context, callID string) error
	// Hangup 挂断指定通话。
	Hangup(ctx context.Context, callID string) error
	// Transfer 将指定通话转接到目标号码。
	Transfer(ctx context.Context, callID, target string) error
	// StartRecording 为指定通话启动录音。
	StartRecording(ctx context.Context, callID string) (Recording, error)
	// StopRecording 停止指定通话的录音。
	StopRecording(ctx context.Context, callID string) error
	// ForkAudio 为指定通话创建音频旁路流。
	ForkAudio(ctx context.Context, callID string) (*AudioStream, error)
	// InjectAudio 向指定通话注入一段音频。
	InjectAudio(ctx context.Context, callID string, chunk AudioChunk) error
	// StopInjectAudio 停止向指定通话注入音频。
	StopInjectAudio(ctx context.Context, callID string) error
}

// MockAdapter 是用于测试和本地开发的内存媒体适配器。
type MockAdapter struct {
	mu         sync.Mutex
	nodeID     string
	calls      map[string]Call
	recordings map[string]Recording
	injected   map[string][]AudioChunk
}

// NewMockAdapter 创建模拟的媒体平面适配器。
func NewMockAdapter(nodeID string) *MockAdapter {
	return &MockAdapter{
		nodeID:     nodeID,
		calls:      map[string]Call{},
		recordings: map[string]Recording{},
		injected:   map[string][]AudioChunk{},
	}
}

// Originate 校验上下文后创建一路 ringing 状态的新通话，并写入内存通话表。
func (m *MockAdapter) Originate(ctx context.Context, req OriginateRequest) (Call, error) {
	if err := ctx.Err(); err != nil {
		return Call{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	call := Call{ID: req.CallID, Caller: req.Caller, Callee: req.Callee, State: CallStateRinging}
	m.calls[req.CallID] = call
	return call, nil
}

// Bridge 将两方通话桥接（状态改为 bridged）。
func (m *MockAdapter) Bridge(ctx context.Context, callID string) error {
	return m.updateCall(ctx, callID, func(call *Call) {
		call.State = CallStateBridged
	})
}

// Hangup 挂断通话（状态改为 hungup）。
func (m *MockAdapter) Hangup(ctx context.Context, callID string) error {
	return m.updateCall(ctx, callID, func(call *Call) {
		call.State = CallStateHungup
	})
}

// Transfer 将通话转移到目标号码。
func (m *MockAdapter) Transfer(ctx context.Context, callID, target string) error {
	return m.updateCall(ctx, callID, func(call *Call) {
		call.Callee = target
	})
}

// StartRecording 校验上下文后为通话创建 active 录音记录。
func (m *MockAdapter) StartRecording(ctx context.Context, callID string) (Recording, error) {
	if err := ctx.Err(); err != nil {
		return Recording{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := Recording{CallID: callID, ObjectKey: "recordings/" + callID + ".wav", Active: true}
	m.recordings[callID] = rec
	return rec, nil
}

// StopRecording 校验上下文后将通话录音记录标记为 inactive。
func (m *MockAdapter) StopRecording(ctx context.Context, callID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.recordings[callID]
	rec.Active = false
	m.recordings[callID] = rec
	return nil
}

// ForkAudio 创建双工音频管道，并用后台协程把 Input 帧转发到 Output。
// 逻辑：先校验 ctx 并记录启动日志，再创建带缓冲的输入/输出通道；后台协程持续读取 Input，成功转发到 Output 后继续等待下一帧，Input 关闭或 ctx 取消时关闭 Output 并退出。
func (m *MockAdapter) ForkAudio(ctx context.Context, callID string) (*AudioStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "媒体音频分流已启动",
		slog.String("nodeId", m.nodeID),
		slog.String("callId", callID),
	)
	stream := &AudioStream{Input: make(chan AudioFrame, 8), Output: make(chan AudioFrame, 8)}
	go func() {
		defer close(stream.Output)
		for frame := range stream.Input {
			slog.DebugContext(ctx, "媒体音频帧已输出",
				slog.String("nodeId", m.nodeID),
				slog.String("callId", frame.CallID),
				slog.Int("audioBytes", len(frame.Payload)),
			)
			select {
			case stream.Output <- frame:
			case <-ctx.Done():
				slog.WarnContext(ctx, "媒体音频分流已停止",
					slog.String("nodeId", m.nodeID),
					slog.String("callId", callID),
					slog.Any("error", ctx.Err()),
				)
				return
			}
		}
		slog.InfoContext(ctx, "媒体音频分流已关闭",
			slog.String("nodeId", m.nodeID),
			slog.String("callId", callID),
		)
	}()
	return stream, nil
}

// InjectAudio 校验上下文后记录注入到通话的音频块，模拟 TTS 播放。
func (m *MockAdapter) InjectAudio(ctx context.Context, callID string, chunk AudioChunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injected[callID] = append(m.injected[callID], chunk)
	slog.InfoContext(ctx, "媒体 TTS 音频已注入",
		slog.String("nodeId", m.nodeID),
		slog.String("callId", callID),
		slog.Int("audioBytes", len(chunk.Payload)),
		slog.Int("injectedChunks", len(m.injected[callID])),
	)
	return nil
}

// StopInjectAudio 校验上下文后清除指定通话的注入音频缓存。
func (m *MockAdapter) StopInjectAudio(ctx context.Context, callID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previousChunks := len(m.injected[callID])
	delete(m.injected, callID)
	slog.InfoContext(ctx, "媒体 TTS 音频注入已停止",
		slog.String("nodeId", m.nodeID),
		slog.String("callId", callID),
		slog.Int("clearedChunks", previousChunks),
	)
	return nil
}

// GetCall 持锁查询通话状态，通话不存在时返回 ErrCallNotFound。
func (m *MockAdapter) GetCall(callID string) (Call, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	call, ok := m.calls[callID]
	if !ok {
		return Call{}, ErrCallNotFound
	}
	return call, nil
}

// InjectedAudio 返回已注入通话的音频块快照（测试用）。
func (m *MockAdapter) InjectedAudio(callID string) []AudioChunk {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AudioChunk(nil), m.injected[callID]...)
}

// updateCall 统一执行通话更新。
// 逻辑：先校验 ctx，再持锁读取通话快照；通话不存在时返回 ErrCallNotFound，存在时把快照交给 update 修改，最后写回内存表保证状态更新原子完成。
func (m *MockAdapter) updateCall(ctx context.Context, callID string, update func(*Call)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	call, ok := m.calls[callID]
	if !ok {
		return ErrCallNotFound
	}
	update(&call)
	m.calls[callID] = call
	return nil
}

// NormalizePBXEvent 将 PBX 原始事件字符串映射为类型化事件。
// 逻辑：按 PBX 原始事件名匹配已知通话生命周期事件，无法识别时保留 raw 内容并归类为 EventUnknown。
func NormalizePBXEvent(raw, callID string) Event {
	switch raw {
	case "CALL_CREATE":
		return Event{Type: EventCallCreated, CallID: callID, Raw: raw}
	case "CALL_ANSWER":
		return Event{Type: EventCallAnswered, CallID: callID, Raw: raw}
	case "CALL_HANGUP":
		return Event{Type: EventCallHangup, CallID: callID, Raw: raw}
	default:
		return Event{Type: EventUnknown, CallID: callID, Raw: raw}
	}
}

