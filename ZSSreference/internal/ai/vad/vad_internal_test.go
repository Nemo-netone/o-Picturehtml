//  语音活动检测(VAD)：Simple(RMS能量门控)+Silero(ONNX模型)双模式
package vad

import (
	"encoding/binary"
	"testing"
)

// TestPCM16LEToFloat32 验证 Silero 输入预处理会把 PCM16LE 归一化到 float32 窗口。
func TestPCM16LEToFloat32(t *testing.T) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:], uint16(int16(32767)))
	minValue := int16(-32768)
	binary.LittleEndian.PutUint16(payload[2:], uint16(minValue))

	samples := pcm16LEToFloat32(payload, 4)
	if len(samples) != 4 {
		t.Fatalf("unexpected sample count: %d", len(samples))
	}
	if samples[0] < 0.99 || samples[1] != -1 || samples[2] != 0 || samples[3] != 0 {
		t.Fatalf("unexpected samples: %#v", samples)
	}
}

// TestPCM16LEPayloadToFloat32DoesNotPad 验证实时 Silero 输入只转换真实样本，不把 WebRTC 20ms 帧补零成 512。
func TestPCM16LEPayloadToFloat32DoesNotPad(t *testing.T) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:], uint16(int16(32767)))
	minValue := int16(-32768)
	binary.LittleEndian.PutUint16(payload[2:], uint16(minValue))

	samples := pcm16LEPayloadToFloat32(payload)
	if len(samples) != 2 {
		t.Fatalf("unexpected sample count: %d", len(samples))
	}
	if samples[0] < 0.99 || samples[1] != -1 {
		t.Fatalf("unexpected samples: %#v", samples)
	}
}

// TestAppendSileroSamplesBuildsRealWindows 验证 320-sample WebRTC 帧会累计成 512-sample Silero 窗口，不靠补零凑数。
func TestAppendSileroSamplesBuildsRealWindows(t *testing.T) {
	state := &session{}
	first := repeatedFloat32(1, 320)
	second := repeatedFloat32(2, 320)

	windows := appendSileroSamples(state, first, 512)
	if len(windows) != 0 {
		t.Fatalf("expected no complete window, got %d", len(windows))
	}
	if len(state.onnxPending) != 320 {
		t.Fatalf("unexpected pending samples: %d", len(state.onnxPending))
	}

	windows = appendSileroSamples(state, second, 512)
	if len(windows) != 1 {
		t.Fatalf("expected one complete window, got %d", len(windows))
	}
	window := windows[0]
	if len(window) != 512 {
		t.Fatalf("unexpected window samples: %d", len(window))
	}
	if window[0] != 1 || window[319] != 1 || window[320] != 2 || window[511] != 2 {
		t.Fatalf("window did not preserve real sample order: first=%v middle=%v last=%v", window[0], window[320], window[511])
	}
	if len(state.onnxPending) != 128 {
		t.Fatalf("unexpected remaining samples: %d", len(state.onnxPending))
	}
	for index, sample := range state.onnxPending {
		if sample != 2 {
			t.Fatalf("unexpected pending sample at %d: %v", index, sample)
		}
	}
}

// TestNormalizeConfigDefaults 验证 Silero provider 的 ONNX 输入输出名和窗口参数有稳定默认值。
func TestNormalizeConfigDefaults(t *testing.T) {
	config := normalizeConfig(Config{Provider: ProviderSilero})
	if config.InputName != "input" || config.StateName != "state" || config.SampleRateName != "sr" {
		t.Fatalf("unexpected input defaults: %#v", config)
	}
	if config.OutputName != "output" || config.NextStateName != "stateN" {
		t.Fatalf("unexpected output defaults: %#v", config)
	}
	if config.SampleRate != 16000 || config.SileroWindowSamples != 512 || config.SileroStateSize != 128 {
		t.Fatalf("unexpected silero defaults: %#v", config)
	}
}

// TestEnergyUsesPCM16RMS 验证 simple VAD 对 PCM16LE 使用 RMS 能量，而不是平均字节值。
func TestEnergyUsesPCM16RMS(t *testing.T) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:], uint16(int16(32767)))
	minValue := int16(-32768)
	binary.LittleEndian.PutUint16(payload[2:], uint16(minValue))

	got := energy(payload)
	if got < 99 {
		t.Fatalf("expected high pcm rms energy, got %f", got)
	}
	if energy([]byte{0, 0, 0, 0}) != 0 {
		t.Fatal("expected zero energy for silence")
	}
}

func repeatedFloat32(value float32, count int) []float32 {
	out := make([]float32, count)
	for index := range out {
		out[index] = value
	}
	return out
}

