//  PBX WebSocket传输层：帧协议+连接管理
package pbxprotocol

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func WebSocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func IsWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerContains(r.Header.Get("Connection"), "upgrade")
}

func headerContains(header, target string) bool {
	for _, item := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func ReadFrame(reader *bufio.Reader, requireMask bool) (byte, []byte, error) {
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
		return 0, nil, fmt.Errorf("websocket frame too large: %d", length)
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

func WriteFrame(writer *bufio.Writer, opcode byte, payload []byte, masked bool) error {
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
	out := payload
	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		if _, err := writer.Write(maskKey[:]); err != nil {
			return err
		}
		out = append([]byte(nil), payload...)
		for i := range out {
			out[i] ^= maskKey[i%4]
		}
	}
	if _, err := writer.Write(out); err != nil {
		return err
	}
	return writer.Flush()
}

