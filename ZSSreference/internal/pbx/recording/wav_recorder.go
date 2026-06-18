package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
)

const (
	defaultPCMFrameBuffer = 256
	defaultSampleRate     = 16000
	defaultChannels       = 1
	defaultBitsPerSample  = 16
)

type PCM16WAVConfig struct {
	TenantID    string
	CallID      string
	RecordingID string
	SampleRate  int
	Buffer      int
}

type PCM16WAVRecorder struct {
	service    *Service
	started    Metadata
	sampleRate int
	channels   int

	frames chan []byte
	done   chan struct{}
	pcm    bytes.Buffer

	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	finalDone chan struct{}
	final     Metadata
	finalErr  error
	dropped   atomic.Int64
}

// StartPCM16WAVRecorder 创建一个会话级 PCM16LE/WAV 录音器。
func StartPCM16WAVRecorder(ctx context.Context, service *Service, cfg PCM16WAVConfig) (*PCM16WAVRecorder, Metadata, error) {
	if service == nil {
		return nil, Metadata{}, fmt.Errorf("recording service is nil")
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = defaultSampleRate
	}
	if cfg.Buffer <= 0 {
		cfg.Buffer = defaultPCMFrameBuffer
	}
	startedAt := time.Now()
	metadata, err := service.Start(ctx, cfg.TenantID, cfg.CallID, cfg.RecordingID, startedAt)
	if err != nil {
		return nil, Metadata{}, err
	}
	recorder := &PCM16WAVRecorder{
		service:    service,
		started:    metadata,
		sampleRate: cfg.SampleRate,
		channels:   defaultChannels,
		frames:     make(chan []byte, cfg.Buffer),
		done:       make(chan struct{}),
		finalDone:  make(chan struct{}),
	}
	go recorder.loop()
	return recorder, metadata, nil
}

// WritePCM16LE 异步追加一帧 PCM16LE 单声道音频。返回 false 表示内部缓冲已满或录音已关闭。
func (r *PCM16WAVRecorder) WritePCM16LE(frame []byte) bool {
	if r == nil || len(frame) == 0 {
		return true
	}
	copied := append([]byte(nil), frame...)
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return false
	}
	select {
	case r.frames <- copied:
		r.mu.RUnlock()
		return true
	default:
		r.mu.RUnlock()
		r.dropped.Add(1)
		return false
	}
}

// Close 停止录音、封装 WAV，并上传到对象存储。
func (r *PCM16WAVRecorder) Close(ctx context.Context) (Metadata, error) {
	if r == nil {
		return Metadata{}, nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		close(r.frames)
		r.mu.Unlock()
		<-r.done
		audio := EncodePCM16WAV(r.pcm.Bytes(), r.sampleRate, r.channels)
		r.final, r.finalErr = r.service.CompleteUpload(ctx, r.started.TenantID, r.started.ID, audio, storage.SHA256(audio), time.Now())
		close(r.finalDone)
	})
	<-r.finalDone
	return r.final, r.finalErr
}

func (r *PCM16WAVRecorder) DroppedFrames() int64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

func (r *PCM16WAVRecorder) loop() {
	defer close(r.done)
	for frame := range r.frames {
		if len(frame)%2 != 0 {
			frame = frame[:len(frame)-1]
		}
		if len(frame) == 0 {
			continue
		}
		if _, err := r.pcm.Write(frame); err != nil {
			slog.Warn("写入录音 PCM buffer 失败", slog.Any("error", err))
		}
	}
}

// EncodePCM16WAV 将 PCM16LE 单声道音频封装为标准 WAV 文件。
func EncodePCM16WAV(pcm []byte, sampleRate, channels int) []byte {
	if sampleRate <= 0 {
		sampleRate = defaultSampleRate
	}
	if channels <= 0 {
		channels = defaultChannels
	}
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	bitsPerSample := defaultBitsPerSample
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	dataSize := uint32(len(pcm))
	riffSize := uint32(36 + len(pcm))

	var out bytes.Buffer
	out.Grow(44 + len(pcm))
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, riffSize)
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&out, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&out, binary.LittleEndian, uint16(bitsPerSample))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, dataSize)
	out.Write(pcm)
	return out.Bytes()
}
