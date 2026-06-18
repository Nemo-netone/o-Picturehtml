//  WebSocket帧协议：低层帧读写+消息序列化
package httpapi

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// upgradeWebSocket 完成 RFC6455 HTTP Upgrade，并返回可读写文本帧的底层连接。
func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !isWebSocketRequest(r) {
		JSONError(w, http.StatusBadRequest, "websocket upgrade required")
		return nil, errors.New("websocket upgrade required")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		JSONError(w, http.StatusBadRequest, "missing websocket key")
		return nil, errors.New("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		JSONError(w, http.StatusInternalServerError, "websocket hijack unsupported")
		return nil, errors.New("websocket hijack unsupported")
	}
	netConn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	accept := websocketAccept(key)
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	return &wsConn{netConn: netConn, reader: rw.Reader, writer: rw.Writer}, nil
}

// isWebSocketRequest 判断请求是否为合法 WebSocket upgrade 请求。
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerContains(r.Header.Get("Connection"), "upgrade")
}

// headerContains 判断逗号分隔 header 中是否包含目标 token。
func headerContains(header, target string) bool {
	for _, item := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

// websocketAccept 根据客户端 Sec-WebSocket-Key 计算服务端握手响应。
func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// readWebSocketFrame 读取一个 WebSocket 帧，服务端读取客户端帧时 requireMask 必须为 true。
func readWebSocketFrame(reader *bufio.Reader, requireMask bool) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	if requireMask && !masked {
		return 0, nil, errors.New("websocket client frame must be masked")
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(reader, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	if length > 8*1024*1024 {
		return 0, nil, errors.New("websocket frame too large")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

// writeWebSocketJSON 将对象编码为 JSON 文本帧发送给客户端。
func writeWebSocketJSON(conn *wsConn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	_ = conn.netConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return writeWebSocketFrame(conn.writer, 0x1, data, false)
}

// writeWebSocketClose 发送 close 控制帧。
func writeWebSocketClose(conn *wsConn) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return writeWebSocketFrame(conn.writer, 0x8, nil, false)
}

// writeWebSocketFrame 写入单个 WebSocket 帧；客户端写入服务端时 masked=true，服务端写回客户端时 masked=false。
func writeWebSocketFrame(writer *bufio.Writer, opcode byte, payload []byte, masked bool) error {
	first := byte(0x80) | opcode
	header := []byte{first, 0}
	length := len(payload)
	switch {
	case length < 126:
		header[1] = byte(length)
	case length <= 0xFFFF:
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(length))
	default:
		header[1] = 127
		header = binary.BigEndian.AppendUint64(header, uint64(length))
	}
	if masked {
		header[1] |= 0x80
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		if _, err := writer.Write(maskKey[:]); err != nil {
			return err
		}
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

