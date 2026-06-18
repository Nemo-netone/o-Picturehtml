//  语音活动检测(VAD)：Simple(RMS能量门控)+Silero(ONNX模型)双模式
package vad_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/ai/vad"
)

// 作用: 验证 Test V A D_ Model Load_ Success 场景的行为。
func TestVAD_ModelLoad_Success(t *testing.T) {
	path := filepath.Join(t.TempDir(), "silero.onnx")
	if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	if _, err := vad.NewDetector(vad.Config{ModelPath: path, SpeechThreshold: 0.5, SpeechMinFrames: 2, SilenceFrames: 2}); err != nil {
		t.Fatalf("new detector: %v", err)
	}
}

// 作用: 验证 Test V A D_ Model Load_ Invalid Path 场景的行为。
func TestVAD_ModelLoad_InvalidPath(t *testing.T) {
	if _, err := vad.NewDetector(vad.Config{Provider: vad.ProviderSilero, ModelPath: "/missing/model.onnx"}); err == nil {
		t.Fatalf("expected invalid model path")
	}
}

// TestVAD_SileroProviderRequiresModel 验证启用 Silero provider 时必须配置模型路径。
func TestVAD_SileroProviderRequiresModel(t *testing.T) {
	if _, err := vad.NewDetector(vad.Config{Provider: vad.ProviderSilero}); err == nil {
		t.Fatal("expected missing silero model path error")
	}
}

// TestVAD_SileroRealModelInference 使用真实 Silero ONNX 模型和 ONNX Runtime 跑静音/语音概率。
func TestVAD_SileroRealModelInference(t *testing.T) {
	modelPath := os.Getenv("SIMULSPEAK_TEST_SILERO_MODEL_PATH")
	runtimePath := os.Getenv("SIMULSPEAK_TEST_ONNX_RUNTIME_LIBRARY_PATH")
	if modelPath == "" || runtimePath == "" {
		t.Skip("set SIMULSPEAK_TEST_SILERO_MODEL_PATH and SIMULSPEAK_TEST_ONNX_RUNTIME_LIBRARY_PATH to run real Silero inference")
	}
	wavPath := os.Getenv("SIMULSPEAK_TEST_SILERO_WAV_PATH")
	if wavPath == "" {
		wavPath = filepath.Join("..", "..", "..", "..", "..", "silero-vad", "tests", "data", "test.wav")
	}
	samples, sampleRate, err := loadWAVPCM16Mono(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}
	d, err := vad.NewDetector(vad.Config{
		Provider:           vad.ProviderSilero,
		ModelPath:          modelPath,
		RuntimeLibraryPath: runtimePath,
		SampleRate:         sampleRate,
	})
	if err != nil {
		t.Fatalf("new silero detector: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})

	silence := pcm16Frame(0, 512)
	var silenceMax float64
	for index := 0; index < 10; index++ {
		score, err := d.Score(vad.Frame{CallID: "silence", Payload: silence})
		if err != nil {
			t.Fatalf("score silence: %v", err)
		}
		if score.Confidence > silenceMax {
			silenceMax = score.Confidence
		}
	}
	if silenceMax > 0.35 {
		t.Fatalf("expected silence confidence below speech threshold, got %.4f", silenceMax)
	}

	var speechMax float64
	frameSamples := 512
	if sampleRate == 8000 {
		frameSamples = 256
	}
	for offset := 0; offset+frameSamples <= len(samples); offset += frameSamples {
		payload := encodePCM16(samples[offset : offset+frameSamples])
		score, err := d.Score(vad.Frame{CallID: "speech", Payload: payload})
		if err != nil {
			t.Fatalf("score speech: %v", err)
		}
		if score.Confidence > speechMax {
			speechMax = score.Confidence
		}
	}
	if speechMax < 0.5 {
		t.Fatalf("expected real wav speech confidence above threshold, got %.4f", speechMax)
	}
	t.Logf("silero real inference: silenceMax=%.4f speechMax=%.4f sampleRate=%d", silenceMax, speechMax, sampleRate)
}

// 作用: 验证 Test V A D_ Speech Start_ Detection 场景的行为。
func TestVAD_SpeechStart_Detection(t *testing.T) {
	d := newDetector(t)
	var events []vad.Event
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: loud()})...)
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: loud()})...)
	if !hasEvent(events, vad.EventSpeechStart) {
		t.Fatalf("expected speech_start, got %#v", events)
	}
}

// 作用: 验证 Test V A D_ Speech End_ Detection 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestVAD_SpeechEnd_Detection(t *testing.T) {
	d := newDetector(t)
	_ = d.Process(vad.Frame{CallID: "call-1", Payload: loud()})
	_ = d.Process(vad.Frame{CallID: "call-1", Payload: loud()})
	var events []vad.Event
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: quiet()})...)
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: quiet()})...)
	if !hasEvent(events, vad.EventSpeechEnd) {
		t.Fatalf("expected speech_end, got %#v", events)
	}
}

// 作用: 验证 Test V A D_ Noise_ Not Trigger A S R 场景的行为。
func TestVAD_Noise_NotTriggerASR(t *testing.T) {
	d := newDetector(t)
	events := d.Process(vad.Frame{CallID: "call-1", Payload: []byte{2, 2, 2}})
	if !hasEvent(events, vad.EventNoise) {
		t.Fatalf("expected noise, got %#v", events)
	}
	if hasEvent(events, vad.EventSpeechStart) {
		t.Fatalf("noise should not trigger speech")
	}
}

// TestVAD_SlidingWindowSpeechStart 验证滑动窗口可以识别非连续但窗口内足够多的语音帧。
func TestVAD_SlidingWindowSpeechStart(t *testing.T) {
	d := newDetector(t)
	var events []vad.Event
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: loud()})...)
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: quiet()})...)
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: loud()})...)
	if !hasEvent(events, vad.EventSpeechStart) {
		t.Fatalf("expected speech_start from sliding window, got %#v", events)
	}
}

// 作用: 验证 Test V A D_ Barge In_ Detection 场景的行为。
func TestVAD_BargeIn_Detection(t *testing.T) {
	d := newDetector(t)
	events := d.Process(vad.Frame{CallID: "call-1", Payload: loud(), TTSPlaying: true})
	events = append(events, d.Process(vad.Frame{CallID: "call-1", Payload: loud(), TTSPlaying: true})...)
	if !hasEvent(events, vad.EventBargeIn) {
		t.Fatalf("expected barge_in, got %#v", events)
	}
}

// 作用: 验证 Test V A D_ Multi Call Isolation 场景的行为。
func TestVAD_MultiCallIsolation(t *testing.T) {
	d := newDetector(t)
	_ = d.Process(vad.Frame{CallID: "call-1", Payload: loud()})
	events := d.Process(vad.Frame{CallID: "call-2", Payload: loud()})
	if hasEvent(events, vad.EventSpeechStart) {
		t.Fatalf("call-2 should not inherit call-1 speech count")
	}
}

// 作用: 验证 Test V A D_ Close_ Releases Session 场景的行为。
func TestVAD_Close_ReleasesSession(t *testing.T) {
	d := newDetector(t)
	_ = d.Process(vad.Frame{CallID: "call-1", Payload: loud()})
	if err := d.Close(); err != nil {
		t.Fatalf("close detector: %v", err)
	}
	if count := d.SessionCount(); count != 0 {
		t.Fatalf("expected sessions cleared, got %d", count)
	}
}

// TestVAD_Reset_ReleasesSingleSession 验证单通话 VAD 状态可在会话关闭时清理。
func TestVAD_Reset_ReleasesSingleSession(t *testing.T) {
	d := newDetector(t)
	_ = d.Process(vad.Frame{CallID: "call-1", Payload: loud()})
	_ = d.Process(vad.Frame{CallID: "call-2", Payload: loud()})
	d.Reset("call-1")
	if count := d.SessionCount(); count != 1 {
		t.Fatalf("expected one remaining session, got %d", count)
	}
}

// 作用: 处理 new Detector 的核心流程。
func newDetector(t *testing.T) *vad.Detector {
	t.Helper()
	path := filepath.Join(t.TempDir(), "silero.onnx")
	_ = os.WriteFile(path, []byte("model"), 0o600)
	d, err := vad.NewDetector(vad.Config{ModelPath: path, SpeechThreshold: 0.5, SpeechMinFrames: 2, SilenceFrames: 2, BargeInFrames: 2, WindowFrames: 3, WindowSpeech: 2})
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	return d
}

// 作用: 处理 loud 的核心流程。
func loud() []byte {
	return []byte{120, 120, 120, 120}
}

func pcm16Frame(sample int16, samples int) []byte {
	out := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		binary.LittleEndian.PutUint16(out[index*2:], uint16(sample))
	}
	return out
}

func encodePCM16(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(out[index*2:], uint16(sample))
	}
	return out
}

func loadWAVPCM16Mono(path string) ([]int16, int, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(payload) < 44 || string(payload[:4]) != "RIFF" || string(payload[8:12]) != "WAVE" {
		return nil, 0, os.ErrInvalid
	}
	reader := bytes.NewReader(payload[12:])
	var sampleRate int
	var channels int
	var data []byte
	for reader.Len() >= 8 {
		var id [4]byte
		if _, err := reader.Read(id[:]); err != nil {
			return nil, 0, err
		}
		var size uint32
		if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
			return nil, 0, err
		}
		if uint32(reader.Len()) < size {
			return nil, 0, os.ErrInvalid
		}
		chunk := make([]byte, size)
		if _, err := reader.Read(chunk); err != nil {
			return nil, 0, err
		}
		if size%2 == 1 && reader.Len() > 0 {
			_, _ = reader.ReadByte()
		}
		switch string(id[:]) {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, os.ErrInvalid
			}
			audioFormat := binary.LittleEndian.Uint16(chunk[0:2])
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSample := binary.LittleEndian.Uint16(chunk[14:16])
			if audioFormat != 1 || bitsPerSample != 16 || channels <= 0 {
				return nil, 0, os.ErrInvalid
			}
		case "data":
			data = chunk
		}
	}
	if sampleRate == 0 || channels == 0 || len(data) == 0 {
		return nil, 0, os.ErrInvalid
	}
	frameBytes := channels * 2
	out := make([]int16, 0, len(data)/frameBytes)
	for offset := 0; offset+frameBytes <= len(data); offset += frameBytes {
		out = append(out, int16(binary.LittleEndian.Uint16(data[offset:])))
	}
	return out, sampleRate, nil
}

// 作用: 处理 quiet 的核心流程。
func quiet() []byte {
	return []byte{0, 0, 0, 0}
}

// 作用: 处理 has Event 的核心流程。
func hasEvent(events []vad.Event, typ vad.EventType) bool {
	for _, event := range events {
		if event.Type == typ {
			return true
		}
	}
	return false
}

