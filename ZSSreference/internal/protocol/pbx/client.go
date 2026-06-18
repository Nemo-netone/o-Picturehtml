//  PBX协议客户端：连接pbx-node WebSocket+消息收发
package pbxprotocol

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Handler func(context.Context, Message)

type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	handler Handler
	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool
}

func Dial(ctx context.Context, rawURL string, handler Handler) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("parse pbx control url: %w", err)
	}
	if parsed.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported pbx control url scheme %q", parsed.Scheme)
	}
	address := parsed.Host
	if !strings.Contains(address, ":") {
		address += ":80"
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial pbx control websocket: %w", err)
	}
	key, err := websocketKey()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	path := parsed.RequestURI()
	if path == "" {
		path = "/"
	}
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", path, parsed.Host, key)
	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write pbx control handshake: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read pbx control handshake: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("pbx control websocket upgrade failed: %s", resp.Status)
	}
	if got := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")); got != WebSocketAccept(key) {
		_ = conn.Close()
		return nil, fmt.Errorf("pbx control websocket accept mismatch")
	}
	client := &Client{
		conn:    conn,
		reader:  reader,
		writer:  bufio.NewWriter(conn),
		handler: handler,
	}
	go client.readLoop()
	return client, nil
}

func (c *Client) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return WriteFrame(c.writer, 0x1, data, true)
}

func (c *Client) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.writeMu.Lock()
	_ = WriteFrame(c.writer, 0x8, nil, true)
	c.writeMu.Unlock()
	return c.conn.Close()
}

func (c *Client) readLoop() {
	ctx := context.Background()
	for {
		opcode, payload, err := ReadFrame(c.reader, false)
		if err != nil {
			return
		}
		switch opcode {
		case 0x1:
			var message Message
			if err := json.Unmarshal(payload, &message); err != nil {
				continue
			}
			if c.handler != nil {
				c.handler(ctx, message)
			}
		case 0x8:
			_ = c.Close()
			return
		case 0x9:
			c.writeMu.Lock()
			_ = WriteFrame(c.writer, 0xA, payload, true)
			c.writeMu.Unlock()
		}
	}
}

func websocketKey() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw[:]), nil
}

