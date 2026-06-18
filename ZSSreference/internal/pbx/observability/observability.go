//  可观测性：指标采集+日志+追踪
package observability

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const TraceParentHeader = "traceparent"

type Labels map[string]string

type Registry struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
}

// NewRegistry 创建内存指标注册表。
func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]float64{},
		gauges:     map[string]float64{},
		histograms: map[string][]float64{},
	}
}

// Inc 递增 counter 指标。
func (r *Registry) Inc(name string, labels Labels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[metricKey(name, labels)] += value
}

// SetGauge 设置 gauge 指标值。
func (r *Registry) SetGauge(name string, labels Labels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[metricKey(name, labels)] = value
}

// Observe 记录 histogram 观测值。
func (r *Registry) Observe(name string, labels Labels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.histograms[metricKey(name, labels)] = append(r.histograms[metricKey(name, labels)], value)
}

// Handler 返回 Prometheus 指标 HTTP 处理器。
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(r.Exposition()))
	})
}

// Exposition 生成 Prometheus 文本格式的指标输出。
func (r *Registry) Exposition() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lines []string
	for key, value := range r.counters {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, value := range r.gauges {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, values := range r.histograms {
		sum := 0.0
		for _, value := range values {
			sum += value
		}
		lines = append(lines, fmt.Sprintf("%s_count %d", key, len(values)))
		lines = append(lines, fmt.Sprintf("%s_sum %g", key, sum))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

type TraceContext struct {
	TraceID string
	SpanID  string
}

// InjectTrace 向 HTTP 请求注入 W3C traceparent 头。
func InjectTrace(req *http.Request, trace TraceContext) {
	if trace.TraceID == "" {
		trace.TraceID = "00000000000000000000000000000000"
	}
	if trace.SpanID == "" {
		trace.SpanID = "0000000000000000"
	}
	req.Header.Set(TraceParentHeader, "00-"+trace.TraceID+"-"+trace.SpanID+"-01")
}

// ExtractTrace 从 HTTP 请求提取 W3C traceparent 头。
func ExtractTrace(req *http.Request) TraceContext {
	value := req.Header.Get(TraceParentHeader)
	parts := strings.Split(value, "-")
	if len(parts) != 4 {
		return TraceContext{}
	}
	return TraceContext{TraceID: parts[1], SpanID: parts[2]}
}

type LogFields struct {
	RequestID   string
	TenantID    string
	CallID      string
	UtteranceID string
	TraceID     string
}

// RequiredLogAttrs 校验必填日志字段是否存在。
func RequiredLogAttrs(fields LogFields) []slog.Attr {
	return []slog.Attr{
		slog.String("requestId", fields.RequestID),
		slog.String("tenantId", fields.TenantID),
		slog.String("callId", fields.CallID),
		slog.String("utteranceId", fields.UtteranceID),
		slog.String("traceId", fields.TraceID),
	}
}

// ValidateLogFields 校验日志字段集合（当前为空）。
func ValidateLogFields(fields LogFields) error {
	if fields.RequestID == "" || fields.TenantID == "" || fields.CallID == "" || fields.TraceID == "" {
		return errors.New("missing required log fields")
	}
	return nil
}

type AlertRule struct {
	Name     string
	Expr     string
	For      time.Duration
	Severity string
}

// DefaultAlertRules 返回内置 Prometheus 告警规则集。
func DefaultAlertRules() []AlertRule {
	return []AlertRule{
		{Name: "NoAvailableNode", Expr: `sum(rate(pbx_route_errors_total{reason="no_available_node"}[5m])) > 0`, For: 2 * time.Minute, Severity: "critical"},
		{Name: "High5xx", Expr: `sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m])) > 0.05`, For: 5 * time.Minute, Severity: "warning"},
		{Name: "ASROutage", Expr: `sum(rate(pbx_provider_errors_total{provider_type="asr"}[5m])) > 1`, For: 2 * time.Minute, Severity: "critical"},
		{Name: "TTSLatency", Expr: `histogram_quantile(0.95, rate(pbx_tts_first_chunk_seconds_bucket[5m])) > 0.5`, For: 5 * time.Minute, Severity: "warning"},
		{Name: "CallSetupFailure", Expr: `sum(rate(pbx_call_setup_failures_total[5m])) > 0`, For: 3 * time.Minute, Severity: "warning"},
		{Name: "BargeInLatency", Expr: `histogram_quantile(0.95, rate(pbx_barge_in_stop_seconds_bucket[5m])) > 0.5`, For: 5 * time.Minute, Severity: "warning"},
	}
}

// ValidateAlertRules 校验告警规则（必填项、名称唯一、括号平衡）。
func ValidateAlertRules(rules []AlertRule) error {
	seen := map[string]bool{}
	for _, rule := range rules {
		if rule.Name == "" || rule.Expr == "" || rule.Severity == "" || rule.For <= 0 {
			return errors.New("invalid alert rule")
		}
		if seen[rule.Name] {
			return errors.New("duplicate alert rule")
		}
		seen[rule.Name] = true
		if strings.Count(rule.Expr, "(") != strings.Count(rule.Expr, ")") ||
			strings.Count(rule.Expr, "{") != strings.Count(rule.Expr, "}") ||
			strings.Count(rule.Expr, "[") != strings.Count(rule.Expr, "]") {
			return fmt.Errorf("alert rule has unbalanced expression: %s", rule.Name)
		}
	}
	return nil
}

// metricKey 构建 Prometheus 指标键：name{label="value"...}。
func metricKey(name string, labels Labels) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, key+`="`+escapeLabel(labels[key])+`"`)
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

// escapeLabel 转义标签值中的反斜杠和双引号。
func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

