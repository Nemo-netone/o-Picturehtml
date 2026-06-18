//  领域模型：ASRResult/TranslationResult/CallSession/Node/Route等核心数据结构
package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

// 作用: 验证 Test Model_ Node_ J S O N Round Trip 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestModel_Node_JSONRoundTrip(t *testing.T) {
	node := model.Node{
		Metadata: model.Metadata{
			Version:   3,
			CreatedAt: time.Unix(1710000000, 0).UTC(),
			UpdatedAt: time.Unix(1710000100, 0).UTC(),
		},
		ID:           "media-01",
		Type:         model.NodeTypeMedia,
		Endpoint:     "10.0.1.11:8021",
		Zone:         "az-a",
		Status:       model.NodeStatusUp,
		Weight:       100,
		MaxCalls:     1000,
		CurrentCalls: 312,
		Version:      "v1.0.0",
		StartedAt:    time.Unix(1710000000, 0).UTC(),
		Capabilities: []string{"sip", "webrtc", "recording"},
		Labels:       map[string]string{"tenant": "shared"},
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}

	var got model.Node
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}

	if got.ID != node.ID || got.Type != node.Type || got.Status != node.Status {
		t.Fatalf("unexpected node round trip: %#v", got)
	}
	if got.Labels["tenant"] != "shared" {
		t.Fatalf("expected labels to round trip")
	}
}

// 作用: 验证 Test Model_ Call_ J S O N Round Trip 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestModel_Call_JSONRoundTrip(t *testing.T) {
	call := model.CallSession{
		Metadata: model.Metadata{Version: 1},
		ID:       "call-1",
		TenantID: "tenant-1",
		Caller:   "1001",
		Callee:   "1002",
		State:    model.CallStateConnected,
		Media:    model.MediaStateConnected,
		Owner: model.CallOwner{
			CallID:      "call-1",
			OwnerNode:   "media-01",
			GatewayNode: "gw-01",
			Epoch:       7,
		},
		GatewayNode: "gw-01",
		MediaNode:   "media-01",
		Participants: []model.Participant{
			{ID: "p-1", Extension: "1001", Role: model.ParticipantRoleCaller},
			{ID: "p-2", Extension: "1002", Role: model.ParticipantRoleCallee},
		},
		AIPipeline: &model.AIPipeline{
			CallID: "call-1",
			VAD:    "vad-01",
			ASR:    "asr-01",
			Agent:  "agent-01",
			TTS:    "tts-01",
			State:  model.AIStateAISpeaking,
		},
	}

	data, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal call: %v", err)
	}

	var got model.CallSession
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal call: %v", err)
	}

	if got.ID != call.ID || got.Owner.Epoch != 7 || got.AIPipeline.Agent != "agent-01" {
		t.Fatalf("unexpected call round trip: %#v", got)
	}
	if len(got.Participants) != 2 {
		t.Fatalf("expected two participants, got %d", len(got.Participants))
	}
}

// 作用: 验证 Test Model_ A I Pipeline_ J S O N Round Trip 场景的行为。
func TestModel_AIPipeline_JSONRoundTrip(t *testing.T) {
	pipeline := model.AIPipeline{
		Metadata:    model.Metadata{Version: 2},
		CallID:      "call-1",
		TenantID:    "tenant-1",
		PolicyID:    "policy-1",
		VAD:         "vad-01",
		ASR:         "asr-01",
		Agent:       "agent-01",
		TTS:         "tts-01",
		State:       model.AIStateUserSpeaking,
		UtteranceID: "utt-1",
	}

	data, err := json.Marshal(pipeline)
	if err != nil {
		t.Fatalf("marshal pipeline: %v", err)
	}

	var got model.AIPipeline
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal pipeline: %v", err)
	}

	if got.CallID != pipeline.CallID || got.State != pipeline.State || got.UtteranceID != "utt-1" {
		t.Fatalf("unexpected pipeline round trip: %#v", got)
	}
}

// 作用: 验证 Test Call State_ Valid Transitions 场景的行为。
func TestCallState_ValidTransitions(t *testing.T) {
	valid := [][2]model.CallState{
		{model.CallStateIdle, model.CallStateRinging},
		{model.CallStateRinging, model.CallStateConnected},
		{model.CallStateRinging, model.CallStateRejected},
		{model.CallStateConnected, model.CallStateEnded},
		{model.CallStateRinging, model.CallStateFailed},
	}

	for _, pair := range valid {
		if !model.CanTransitionCallState(pair[0], pair[1]) {
			t.Fatalf("expected %s -> %s to be valid", pair[0], pair[1])
		}
	}
}

// 作用: 验证 Test Call State_ Invalid Transitions 场景的行为。
func TestCallState_InvalidTransitions(t *testing.T) {
	invalid := [][2]model.CallState{
		{model.CallStateConnected, model.CallStateRinging},
		{model.CallStateEnded, model.CallStateConnected},
		{model.CallStateRejected, model.CallStateConnected},
		{model.CallStateFailed, model.CallStateRinging},
	}

	for _, pair := range invalid {
		if model.CanTransitionCallState(pair[0], pair[1]) {
			t.Fatalf("expected %s -> %s to be invalid", pair[0], pair[1])
		}
	}
}

// 作用: 验证 Test A I State_ Barge In Transition 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestAIState_BargeInTransition(t *testing.T) {
	if !model.CanTransitionAIState(model.AIStateAISpeaking, model.AIStateBargeIn) {
		t.Fatalf("expected ai_speaking -> barge_in to be valid")
	}
	if !model.CanTransitionAIState(model.AIStateBargeIn, model.AIStateUserSpeaking) {
		t.Fatalf("expected barge_in -> user_speaking to be valid")
	}
	if model.CanTransitionAIState(model.AIStateIdle, model.AIStateBargeIn) {
		t.Fatalf("expected idle -> barge_in to be invalid")
	}
}

// 作用: 验证 Test Capability_ Match Language Model 场景的行为。
func TestCapability_MatchLanguageModel(t *testing.T) {
	capability := model.Capability{
		ID:                 "asr-01",
		Type:               model.CapabilityTypeASR,
		Protocol:           "grpc",
		Languages:          []string{"zh-CN", "en-US"},
		Models:             []string{"paraformer-streaming"},
		Zone:               "az-a",
		Tenants:            []string{"tenant-1"},
		MaxConcurrency:     32,
		CurrentConcurrency: 8,
	}

	selector := model.CapabilitySelector{
		Type:     model.CapabilityTypeASR,
		Protocol: "grpc",
		Language: "zh-CN",
		Model:    "paraformer-streaming",
		Zone:     "az-a",
		TenantID: "tenant-1",
	}

	if !capability.Matches(selector) {
		t.Fatalf("expected capability to match selector")
	}

	selector.Language = "ja-JP"
	if capability.Matches(selector) {
		t.Fatalf("expected language mismatch to fail")
	}
}

// 作用: 验证 Test Capability_ Provider Config_ J S O N Round Trip 场景的行为。
func TestCapability_ProviderConfig_JSONRoundTrip(t *testing.T) {
	capability := model.Capability{
		ID:       "asr-tencent",
		Type:     model.CapabilityTypeASR,
		Protocol: "websocket",
		ProviderConfig: &model.ProviderConfig{
			Provider: "tencent-asr",
			Endpoint: "wss://asr.cloud.tencent.com/asr/v2",
			Params:   map[string]string{"engine_model_type": "16k_en"},
			Secrets:  map[string]string{"secretKey": "secret-key"},
		},
	}

	data, err := json.Marshal(capability)
	if err != nil {
		t.Fatalf("marshal capability: %v", err)
	}
	var got model.Capability
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal capability: %v", err)
	}
	if got.ProviderConfig == nil || got.ProviderConfig.Provider != "tencent-asr" || got.ProviderConfig.Secrets["secretKey"] != "secret-key" {
		t.Fatalf("unexpected provider config round trip: %#v", got.ProviderConfig)
	}
}

// TestProviderConfig_MergeKeepsDefaultSecrets 验证客户端覆盖少量参数时不会冲掉服务端默认密钥。
func TestProviderConfig_MergeKeepsDefaultSecrets(t *testing.T) {
	defaults := map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {
			Provider: "tencent-asr",
			Params:   map[string]string{"appId": "1250000000", "engine_model_type": "16k_en"},
			Secrets:  map[string]string{"secretId": "secret-id", "secretKey": "secret-key"},
		},
	}
	overrides := map[model.CapabilityType]model.ProviderConfig{
		model.CapabilityTypeASR: {
			Params: map[string]string{"forward_partial": "true"},
		},
	}

	merged := model.MergeProviderConfigs(defaults, overrides)
	asrConfig := merged[model.CapabilityTypeASR]
	if asrConfig.Provider != "tencent-asr" {
		t.Fatalf("provider should come from defaults: %#v", asrConfig)
	}
	if asrConfig.Params["appId"] != "1250000000" || asrConfig.Params["forward_partial"] != "true" {
		t.Fatalf("params not merged: %#v", asrConfig.Params)
	}
	if asrConfig.Secrets["secretId"] != "secret-id" || asrConfig.Secrets["secretKey"] != "secret-key" {
		t.Fatalf("secrets not preserved: %#v", asrConfig.Secrets)
	}

	defaults[model.CapabilityTypeASR].Params["appId"] = "mutated"
	if merged[model.CapabilityTypeASR].Params["appId"] != "1250000000" {
		t.Fatalf("merged config should be isolated from defaults: %#v", merged[model.CapabilityTypeASR].Params)
	}
}

