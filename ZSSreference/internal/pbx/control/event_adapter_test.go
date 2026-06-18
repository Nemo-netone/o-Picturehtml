// PBX WebSocket控制通道：事件适配+服务端
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/eventbus"
	"github.com/SATA260/SimulSpeak1/internal/model"
	pbxevents "github.com/SATA260/SimulSpeak1/internal/pbx/events"
	pbxprotocol "github.com/SATA260/SimulSpeak1/internal/protocol/pbx"
)

func TestEventAdapterCachesICEUntilAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus, adapter, reader, cleanup := newAdapterTest(t, ctx)
	defer cleanup()
	adapter.BindConnection("conn-1")

	if err := bus.PublishBestEffort(ctx, pbxevents.Topic, pbxevents.NewICECandidateEvent(pbxevents.ICECandidatePayload{
		ConnectionID: "conn-1",
		CallID:       "call-1",
		UserID:       "user-1",
		Candidate:    "candidate-before-answer",
	})); err != nil {
		t.Fatalf("publish ice: %v", err)
	}
	if err := bus.PublishRequired(ctx, pbxevents.Topic, pbxevents.NewWebRTCAnswerEvent(pbxevents.WebRTCAnswerPayload{
		RequestID:    "req-1",
		ConnectionID: "conn-1",
		CallID:       "call-1",
		UserID:       "user-1",
		SDP:          "answer-sdp",
	})); err != nil {
		t.Fatalf("publish answer: %v", err)
	}

	answer := readAdapterMessage(t, reader)
	if answer.Type != TypeWebRTCAnswer || answer.SDP != "answer-sdp" {
		t.Fatalf("unexpected answer: %#v", answer)
	}
	ice := readAdapterMessage(t, reader)
	if ice.Type != TypeICE || ice.Candidate != "candidate-before-answer" {
		t.Fatalf("unexpected ice: %#v", ice)
	}
}

func TestEventAdapterConvertsPBXEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus, adapter, reader, cleanup := newAdapterTest(t, ctx)
	defer cleanup()
	adapter.BindConnection("conn-1")

	events := []model.DomainEvent{
		pbxevents.NewASRResultEvent(pbxevents.ASRResultPayload{
			ConnectionID: "conn-1",
			CallID:       "call-1",
			UserID:       "user-1",
			UtteranceID:  "utt-1",
			Text:         "hello",
			IsFinal:      true,
			Confidence:   0.9,
			Language:     "en",
			Metadata:     map[string]string{"provider": "test-asr"},
		}),
		pbxevents.NewTranslationResultEvent(pbxevents.TranslationResultPayload{
			ConnectionID: "conn-1",
			CallID:       "call-1",
			UserID:       "user-1",
			UtteranceID:  "utt-1",
			SourceText:   "hello",
			Text:         "你好",
			IsFinal:      true,
			Engine:       "tmt",
			Language:     "zh",
			Metadata:     map[string]string{"provider": "test-tmt"},
		}),
		pbxevents.NewTTSResultEvent(pbxevents.TTSResultPayload{
			RequestID:    "tts-1",
			ConnectionID: "conn-1",
			CallID:       "call-1",
			UserID:       "user-1",
			UtteranceID:  "utt-1",
			Text:         "你好",
			Format:       "pcmu",
			SampleRate:   8000,
			IsLast:       true,
			Language:     "zh-CN",
			Sequence:     7,
		}),
		pbxevents.NewErrorEvent(pbxevents.ErrorPayload{
			RequestID:    "err-1",
			ConnectionID: "conn-1",
			CallID:       "call-1",
			UserID:       "user-1",
			Error:        "boom",
		}),
	}
	for _, event := range events {
		if err := bus.PublishRequired(ctx, pbxevents.Topic, event); err != nil {
			t.Fatalf("publish %s: %v", event.Type, err)
		}
	}

	asr := readAdapterMessage(t, reader)
	if asr.Type != TypeASRResult || asr.Text != "hello" || !asr.IsFinal || asr.Metadata["provider"] != "test-asr" {
		t.Fatalf("unexpected asr message: %#v", asr)
	}
	translation := readAdapterMessage(t, reader)
	if translation.Type != TypeTranslationResult || translation.SourceText != "hello" || translation.Text != "你好" || translation.Engine != "tmt" {
		t.Fatalf("unexpected translation message: %#v", translation)
	}
	tts := readAdapterMessage(t, reader)
	if tts.Type != TypeTTSResult || tts.Text != "你好" || tts.Format != "pcmu" || tts.SampleRate != 8000 || tts.Sequence != 7 {
		t.Fatalf("unexpected tts message: %#v", tts)
	}
	errMessage := readAdapterMessage(t, reader)
	if errMessage.Type != TypeError || errMessage.Error != "boom" {
		t.Fatalf("unexpected error message: %#v", errMessage)
	}
}

func TestServerAppliesSessionLanguageDefaultsToTTS(t *testing.T) {
	language, err := model.NormalizeSessionLanguageOptions(map[string]string{
		"sourceLanguage": "zh",
		"targetLanguage": "en",
		"ttsVoiceType":   "101050",
	})
	if err != nil {
		t.Fatalf("normalize language: %v", err)
	}
	server := &Server{}
	server.setSessionLanguage("conn-1", language)

	message := server.applyTTSLanguageDefaults(Message{ConnectionID: "conn-1"})
	if message.Language != "en-US" || message.Voice != "101050" || message.Metadata["ttsPrimaryLanguage"] != "2" {
		t.Fatalf("unexpected tts defaults: %#v", message)
	}
	config := ttsConfigFromProviderConfig(model.ProviderConfig{Provider: "tencent-tts"}, message)
	if config.DefaultLanguage != "en-US" || config.DefaultVoice != "101050" || config.Params["primaryLanguage"] != "2" {
		t.Fatalf("unexpected tts config: %#v", config)
	}
}

func newAdapterTest(t *testing.T, ctx context.Context) (*eventbus.MemoryBus, *eventAdapter, *bufio.Reader, func()) {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	bus := eventbus.NewMemoryBus()
	conn := &serverConn{
		netConn: serverSide,
		reader:  bufio.NewReader(serverSide),
		writer:  bufio.NewWriter(serverSide),
	}
	adapter := newEventAdapter(bus, conn)
	adapter.Start(ctx)
	cleanup := func() {
		adapter.Stop()
		_ = clientSide.Close()
		_ = serverSide.Close()
	}
	return bus, adapter, bufio.NewReader(clientSide), cleanup
}

func readAdapterMessage(t *testing.T, reader *bufio.Reader) Message {
	t.Helper()
	type frameResult struct {
		opcode  byte
		payload []byte
		err     error
	}
	done := make(chan frameResult, 1)
	go func() {
		opcode, payload, err := pbxprotocol.ReadFrame(reader, false)
		done <- frameResult{opcode: opcode, payload: payload, err: err}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("read frame: %v", result.err)
		}
		if result.opcode != 0x1 {
			t.Fatalf("unexpected opcode: %d", result.opcode)
		}
		var message Message
		if err := json.Unmarshal(result.payload, &message); err != nil {
			t.Fatalf("decode message: %v", err)
		}
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for adapter message")
	}
	return Message{}
}
