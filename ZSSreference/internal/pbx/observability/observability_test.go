//  可观测性：指标采集+日志+追踪
package observability_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/pbx/observability"
)

// 作用: 验证 Test Metrics_ Exposed 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestMetrics_Exposed(t *testing.T) {
	registry := observability.NewRegistry()
	registry.SetGauge("pbx_active_calls", observability.Labels{"tenantId": "tenant-a", "node": "media-1"}, 2)
	registry.Inc("pbx_ws_connections_total", observability.Labels{"tenantId": "tenant-a"}, 1)
	registry.Observe("pbx_provider_latency_seconds", observability.Labels{"provider": "asr", "tenantId": "tenant-a"}, 0.12)

	rec := httptest.NewRecorder()
	registry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`pbx_active_calls{node="media-1",tenantId="tenant-a"} 2`,
		`pbx_ws_connections_total{tenantId="tenant-a"} 1`,
		`pbx_provider_latency_seconds{provider="asr",tenantId="tenant-a"}_count 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, body)
		}
	}
}

// 作用: 验证 Test Trace_ Propagation H T T P To Provider 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestTrace_PropagationHTTPToProvider(t *testing.T) {
	trace := observability.TraceContext{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef"}
	// 作用: 测试服务端接收请求并断言 traceparent 已透传。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got := observability.ExtractTrace(req)
		if got.TraceID != trace.TraceID || got.SpanID != trace.SpanID {
			t.Fatalf("trace not propagated: %#v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	observability.InjectTrace(req, trace)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_ = resp.Body.Close()
}

// 作用: 验证 Test Logs_ Required Fields 场景的行为。
// 逻辑: 先构造测试上下文，再执行目标行为，最后断言关键结果。
func TestLogs_RequiredFields(t *testing.T) {
	fields := observability.LogFields{RequestID: "req-1", TenantID: "tenant-a", CallID: "call-1", UtteranceID: "utt-1", TraceID: "trace-1"}
	if err := observability.ValidateLogFields(fields); err != nil {
		t.Fatalf("validate fields: %v", err)
	}
	attrs := observability.RequiredLogAttrs(fields)
	got := map[string]string{}
	for _, attr := range attrs {
		if attr.Value.Kind() == slog.KindString {
			got[attr.Key] = attr.Value.String()
		}
	}
	for _, key := range []string{"requestId", "tenantId", "callId", "utteranceId", "traceId"} {
		if got[key] == "" {
			t.Fatalf("missing log field %s in %#v", key, got)
		}
	}
}

// 作用: 验证 Test Alert Rules_ Syntax 场景的行为。
func TestAlertRules_Syntax(t *testing.T) {
	rules := observability.DefaultAlertRules()
	if len(rules) < 6 {
		t.Fatalf("expected default alert rules")
	}
	if err := observability.ValidateAlertRules(rules); err != nil {
		t.Fatalf("validate alert rules: %v", err)
	}
}

