//  安全：鉴权+加密+令牌管理
package security

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrSSRFBlocked       = errors.New("ssrf target blocked")
	ErrRateLimitExceeded = errors.New("auth rate limit exceeded")
)

var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|password|secret|token)=([^,\s&]+)`)

// RedactSecrets 用正则替换日志中的敏感字段（api_key、token、password 等）为 [REDACTED]。
func RedactSecrets(value string) string {
	return secretPattern.ReplaceAllString(value, `$1=[REDACTED]`)
}

// RedactMap 脱敏 map 中的敏感值，敏感 key 整个 value 替换为 [REDACTED]。
func RedactMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		if isSecretKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = RedactSecrets(value)
	}
	return out
}

// ValidateProviderURL 校验外部 URL 安全：仅允许 HTTP/HTTPS，禁止 localhost 和私有 IP（防 SSRF）。
func ValidateProviderURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrSSRFBlocked
	}
	host := parsed.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return ErrSSRFBlocked
	}
	ip := net.ParseIP(host)
	if ip != nil && isPrivateIP(ip) {
		return ErrSSRFBlocked
	}
	return nil
}

type SafeTransport struct {
	Base http.RoundTripper
}

// RoundTrip 包装 http.RoundTripper，每次请求前校验 URL 安全性。
func (t SafeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := ValidateProviderURL(req.URL.String()); err != nil {
		return nil, err
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// SecurityHeaders 返回 HTTP 中间件：为每个请求注入安全头（X-Content-Type-Options、X-Frame-Options、CSP）并支持 CORS。
func SecurityHeaders(allowedOrigins []string) func(http.Handler) http.Handler {
	allow := map[string]bool{}
	for _, origin := range allowedOrigins {
		allow[origin] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			if origin := req.Header.Get("Origin"); allow[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			next.ServeHTTP(w, req)
		})
	}
}

type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	records map[string][]time.Time
}

// NewRateLimiter 创建认证接口的滑动窗口限流器。
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &RateLimiter{limit: limit, window: window, records: map[string][]time.Time{}}
}

// Allow 滑动窗口限流检查，超出限制返回 ErrRateLimitExceeded。
func (r *RateLimiter) Allow(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	records := r.records[key]
	kept := records[:0]
	for _, at := range records {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= r.limit {
		r.records[key] = kept
		return ErrRateLimitExceeded
	}
	r.records[key] = append(kept, now)
	return nil
}

// isSecretKey 判断 key 是否属于敏感信息的键名。
func isSecretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "authorization") ||
		strings.Contains(key, "key") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "password")
}

// isPrivateIP 检查 IP 是否为私网/回环/链路本地地址。
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 169 && ip4[1] == 254
	}
	return false
}

