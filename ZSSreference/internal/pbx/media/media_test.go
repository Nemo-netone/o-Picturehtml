//  媒体处理：Opus编解码+PCM重采样+音频帧管理
package media_test

import (
	"context"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/pbx/media"
)

// TestMediaAdapterMock_OriginateBridgeHangup 验证模拟适配器能完成外呼、桥接和挂断状态流转。
func TestMediaAdapterMock_OriginateBridgeHangup(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	call, err := adapter.Originate(context.Background(), media.OriginateRequest{CallID: "call-1", Caller: "1001", Callee: "1002"})
	if err != nil {
		t.Fatalf("originate: %v", err)
	}
	if call.State != media.CallStateRinging {
		t.Fatalf("expected ringing, got %s", call.State)
	}
	if err := adapter.Bridge(context.Background(), "call-1"); err != nil {
		t.Fatalf("bridge: %v", err)
	}
	if err := adapter.Hangup(context.Background(), "call-1"); err != nil {
		t.Fatalf("hangup: %v", err)
	}
	got, _ := adapter.GetCall("call-1")
	if got.State != media.CallStateHungup {
		t.Fatalf("expected hungup, got %s", got.State)
	}
}

// TestMediaAdapterMock_Transfer 验证模拟适配器能把通话转接到新的被叫号码。
func TestMediaAdapterMock_Transfer(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	_, _ = adapter.Originate(context.Background(), media.OriginateRequest{CallID: "call-1", Caller: "1001", Callee: "1002"})
	if err := adapter.Transfer(context.Background(), "call-1", "1003"); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	got, _ := adapter.GetCall("call-1")
	if got.Callee != "1003" {
		t.Fatalf("expected callee 1003, got %s", got.Callee)
	}
}

// TestMediaAdapterMock_RecordStartStop 验证模拟适配器能启动录音并生成对象键。
func TestMediaAdapterMock_RecordStartStop(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	_, _ = adapter.Originate(context.Background(), media.OriginateRequest{CallID: "call-1", Caller: "1001", Callee: "1002"})
	rec, err := adapter.StartRecording(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("start recording: %v", err)
	}
	if rec.ObjectKey == "" {
		t.Fatalf("expected object key")
	}
	if err := adapter.StopRecording(context.Background(), "call-1"); err != nil {
		t.Fatalf("stop recording: %v", err)
	}
}

// TestMediaAdapterMock_ForkAudioStream 验证音频旁路流会把输入帧转发到输出通道。
func TestMediaAdapterMock_ForkAudioStream(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	stream, err := adapter.ForkAudio(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("fork audio: %v", err)
	}
	stream.Input <- media.AudioFrame{CallID: "call-1", Payload: []byte("pcm")}
	frame := <-stream.Output
	if string(frame.Payload) != "pcm" {
		t.Fatalf("unexpected payload: %s", frame.Payload)
	}
}

// TestMediaAdapterMock_InjectAudio 验证模拟适配器会记录注入到通话的音频块。
func TestMediaAdapterMock_InjectAudio(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	if err := adapter.InjectAudio(context.Background(), "call-1", media.AudioChunk{CallID: "call-1", Payload: []byte("tts")}); err != nil {
		t.Fatalf("inject audio: %v", err)
	}
	chunks := adapter.InjectedAudio("call-1")
	if len(chunks) != 1 || string(chunks[0].Payload) != "tts" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

// TestMediaAdapterMock_StopInjectOnBargeIn 验证停止注入会清空通话的缓存音频块。
func TestMediaAdapterMock_StopInjectOnBargeIn(t *testing.T) {
	adapter := media.NewMockAdapter("media-01")
	_ = adapter.InjectAudio(context.Background(), "call-1", media.AudioChunk{CallID: "call-1", Payload: []byte("tts")})
	if err := adapter.StopInjectAudio(context.Background(), "call-1"); err != nil {
		t.Fatalf("stop inject: %v", err)
	}
	if chunks := adapter.InjectedAudio("call-1"); len(chunks) != 0 {
		t.Fatalf("expected chunks cleared, got %#v", chunks)
	}
}

// TestMediaEvent_NormalizePBXEvent 验证 PBX 原始事件能归一化为类型化媒体事件。
func TestMediaEvent_NormalizePBXEvent(t *testing.T) {
	event := media.NormalizePBXEvent("CALL_ANSWER", "call-1")
	if event.Type != media.EventCallAnswered || event.CallID != "call-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

