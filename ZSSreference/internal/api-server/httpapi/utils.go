// HTTP API工具函数
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// JSON 写入成功响应。
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Code: status, Message: "ok", Data: data})
}

// JSONError 写入错误响应。
func JSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Code: status, Message: "error", Error: message})
}

// decodeJSON 从请求体解码 JSON 到 target。
func decodeJSON(r *http.Request, target any) error {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func queryInt(r *http.Request, name string, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("query %s must be an integer", name)
	}
	return parsed, nil
}

func decodeJSONObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func decodeProviderIDMap(raw string) map[string][]int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string][]int64
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func decodeJSONAny(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return raw
	}
	return out
}
