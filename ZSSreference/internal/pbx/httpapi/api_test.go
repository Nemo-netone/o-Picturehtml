//  PBX HTTP/WS API：对外暴露的pbx-node控制面接口
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/pbx/httpapi"
	"github.com/SATA260/SimulSpeak1/internal/pbx/webrtc"
)

func TestHTTP_Health(t *testing.T) {
	api := httpapi.New(httpapi.Dependencies{WebRTC: webrtc.NewManager()})
	rec := httptest.NewRecorder()

	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pbx/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"service":"pbx-node"`) {
		t.Fatalf("unexpected health body: %s", rec.Body.String())
	}
}

func TestHTTP_WebSocketRouteRequiresUpgrade(t *testing.T) {
	api := httpapi.New(httpapi.Dependencies{WebRTC: webrtc.NewManager()})
	rec := httptest.NewRecorder()

	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pbx/ws", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_NotFound(t *testing.T) {
	api := httpapi.New(httpapi.Dependencies{WebRTC: webrtc.NewManager()})
	rec := httptest.NewRecorder()

	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

