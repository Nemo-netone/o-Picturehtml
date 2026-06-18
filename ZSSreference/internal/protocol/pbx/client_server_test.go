//  api-server↔pbx-node WebSocket协议：消息类型定义+客户端+WebSocket传输
package pbxprotocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientServerPingPong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(serveProtocolPingPong))
	t.Cleanup(server.Close)

	messages := make(chan Message, 1)
	client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), func(ctx context.Context, message Message) {
		messages <- message
	})
	if err != nil {
		t.Fatalf("dial control websocket: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Send(context.Background(), Message{Type: TypePing, RequestID: "ping-1", ConnectionID: "conn-1"}); err != nil {
		t.Fatalf("send ping: %v", err)
	}

	select {
	case message := <-messages:
		if message.Type != TypePong || message.RequestID != "ping-1" || message.ConnectionID != "conn-1" {
			t.Fatalf("unexpected pong: %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pong")
	}
}

func serveProtocolPingPong(w http.ResponseWriter, r *http.Request) {
	if !IsWebSocketRequest(r) {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	hijacker, ok := w.(http.Hijacker)
	if key == "" || !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusBadRequest)
		return
	}
	netConn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func(netConn net.Conn) { _ = netConn.Close() }(netConn)
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", WebSocketAccept(key)); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	reader := rw.Reader
	writer := bufio.NewWriter(netConn)
	for {
		opcode, payload, err := ReadFrame(reader, true)
		if err != nil {
			return
		}
		if opcode != 0x1 {
			continue
		}
		var message Message
		if err := json.Unmarshal(payload, &message); err != nil {
			return
		}
		if message.Type != TypePing {
			continue
		}
		data, _ := json.Marshal(Message{Type: TypePong, RequestID: message.RequestID, ConnectionID: message.ConnectionID})
		_ = WriteFrame(writer, 0x1, data, false)
	}
}

