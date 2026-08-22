package cdp

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A small WebSocket client, enough for the DevTools protocol over loopback.
//
// It exists to remove the last third-party dependency from the browser side.
// gobwas/ws is a fine library, but it pulls in gobwas/pool, which references
// golang.org/x/sys - and vendoring that dragged 7.2 MB of Unix syscall tables
// into a Windows-only tool. That weight is what pushed the deliverable past a
// mail size limit, on a machine where email was the only route left.
//
// Scope is deliberately narrow: ws:// to localhost, client side only. No TLS,
// no extensions, no compression - none of which DevTools uses.

// wsMagic is the GUID RFC 6455 appends to the client key before hashing. It
// is easy to mistype and the failure is opaque - the server simply answers
// with an accept value that does not match, which reads like a handshake
// problem rather than a typo.
const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
}

// wsDial performs the HTTP upgrade handshake and returns a live connection.
func wsDial(rawURL string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("bad websocket url: %w", err)
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("only ws:// is supported, got %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "80")
	}

	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		conn.Close()
		return nil, err
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + encodedKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"

	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Time{})

	br := bufio.NewReaderSize(conn, 64*1024)
	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading handshake: %w", err)
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, fmt.Errorf("websocket upgrade refused: %s",
			strings.TrimSpace(status))
	}

	// Read the headers, checking the accept value proves the server really
	// completed a WebSocket handshake rather than returning some other 101.
	sum := sha1.Sum([]byte(encodedKey + wsMagic))
	wantAccept := base64.StdEncoding.EncodeToString(sum[:])
	sawAccept := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("reading handshake headers: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		name, value, found := strings.Cut(trimmed, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			if strings.TrimSpace(value) != wantAccept {
				conn.Close()
				return nil, fmt.Errorf("websocket handshake failed verification: " +
					"the server answered for a different key")
			}
			sawAccept = true
		}
	}
	if !sawAccept {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake had no accept header")
	}
	_ = conn.SetReadDeadline(time.Time{})

	return &wsConn{conn: conn, br: br}, nil
}

// WriteText sends one unfragmented text frame.
//
// A client frame must always be masked, per the protocol.
func (c *wsConn) WriteText(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var header []byte
	header = append(header, 0x80|opText) // FIN + text

	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(0x80|n))
	case n <= 0xFFFF:
		header = append(header, 0x80|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		header = append(header, ext[:]...)
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}

	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		return err
	}
	return nil
}

// ReadMessage returns the next complete data message, reassembling
// continuation frames and answering pings along the way.
func (c *wsConn) ReadMessage() ([]byte, error) {
	var message []byte
	var messageOp byte

	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}

		switch opcode {
		case opPing:
			if err := c.writeControl(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeControl(opClose, nil)
			return nil, io.EOF
		case opText, opBinary:
			if message != nil {
				return nil, fmt.Errorf("websocket: new message before the last finished")
			}
			messageOp = opcode
			message = payload
		case opContinuation:
			if message == nil {
				return nil, fmt.Errorf("websocket: continuation with nothing to continue")
			}
			message = append(message, payload...)
		default:
			return nil, fmt.Errorf("websocket: unexpected opcode %d", opcode)
		}

		if fin {
			_ = messageOp
			return message, nil
		}
	}
}

// readFrame reads one frame off the wire.
func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	length := uint64(head[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	// A frame this large is not something DevTools sends; refusing beats
	// allocating on a corrupted length.
	const maxFrame = 512 << 20
	if length > maxFrame {
		err = fmt.Errorf("websocket: frame of %d bytes is implausible", length)
		return
	}

	var mask [4]byte
	if masked {
		// Servers must not mask, but handle it rather than corrupting the read.
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// writeControl sends a control frame (ping, pong, close).
func (c *wsConn) writeControl(opcode byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame := []byte{0x80 | opcode, byte(0x80 | len(payload))}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := c.conn.Write(frame)
	return err
}

// Close shuts the connection down, politely if it can.
func (c *wsConn) Close() error {
	_ = c.writeControl(opClose, nil)
	return c.conn.Close()
}
