// Package serve is the loopback HTTP and WebSocket adapter over the core
// and ledger. It binds to
// 127.0.0.1 only — a local adapter for native clients, not a remote server.
package serve

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// wsGUID is the WebSocket protocol GUID from RFC 6455.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxMessage caps one inbound message, whether one frame or assembled from
// fragments, so a declared length is refused before it is allocated.
const maxMessage = 1 << 20

// wsWriteTimeout bounds one frame write, so a client that stops reading is
// closed instead of blocking every broadcast.
const wsWriteTimeout = 5 * time.Second

// Close status codes from RFC 6455 section 7.4.1.
const (
	closeNormal   uint16 = 1000
	closeProtocol uint16 = 1002
	closeTooBig   uint16 = 1009
	closeInternal uint16 = 1011
)

var (
	errTooBig   = fmt.Errorf("message exceeds %d bytes", maxMessage)
	errProtocol = errors.New("websocket protocol violation")
)

// wsOpCode is a WebSocket frame opcode.
type wsOpCode byte

const (
	opContinuation wsOpCode = 0x0
	opText         wsOpCode = 0x1
	opBinary       wsOpCode = 0x2
	opClose        wsOpCode = 0x8
	opPing         wsOpCode = 0x9
	opPong         wsOpCode = 0xA
)

// wsFrame is one parsed WebSocket frame.
type wsFrame struct {
	fin     bool
	opcode  wsOpCode
	payload []byte
}

// wsConn is one WebSocket connection. Frames are written from the handler
// and the broadcaster, so writes hold wmu.
type wsConn struct {
	conn         net.Conn
	writeTimeout time.Duration
	wmu          sync.Mutex
	closed       bool
}

func newWSConn(nc net.Conn) *wsConn {
	return &wsConn{conn: nc, writeTimeout: wsWriteTimeout}
}

// wsKey returns the Sec-WebSocket-Key of a valid upgrade request.
func wsKey(r *http.Request) (string, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return "", errors.New("not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return "", errors.New("missing Sec-WebSocket-Key")
	}
	return key, nil
}

// wsHandshake hijacks the connection and completes the upgrade. On error
// the connection is already closed.
func wsHandshake(hj http.Hijacker, key string) (*wsConn, error) {
	nc, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", wsAccept(key))
	if err == nil {
		err = brw.Flush()
	}
	if err != nil {
		nc.Close()
		return nil, err
	}
	return newWSConn(nc), nil
}

// wsAccept computes the Sec-WebSocket-Accept value from the client key.
func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readFrame reads one WebSocket frame, refusing one longer than maxMessage
// before reading its payload.
func (c *wsConn) readFrame() (wsFrame, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return wsFrame{}, err
	}
	fin := hdr[0]&0x80 != 0
	opcode := wsOpCode(hdr[0] & 0x0F)
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.conn, ext[:]); err != nil {
			return wsFrame{}, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.conn, ext[:]); err != nil {
			return wsFrame{}, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > maxMessage {
		return wsFrame{}, errTooBig
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.conn, mask[:]); err != nil {
			return wsFrame{}, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return wsFrame{}, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return wsFrame{fin: fin, opcode: opcode, payload: payload}, nil
}

// writeFrame writes one unmasked frame within the write timeout.
func (c *wsConn) writeFrame(opcode wsOpCode, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	return c.writeFrameLocked(opcode, payload)
}

// writeFrameLocked writes one frame; the caller holds wmu.
func (c *wsConn) writeFrameLocked(opcode wsOpCode, payload []byte) error {
	frame := []byte{byte(0x80 | opcode)}
	switch {
	case len(payload) < 126:
		frame = append(frame, byte(len(payload)))
	case len(payload) < 65536:
		frame = append(frame, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		frame = append(append(frame, 127), ext[:]...)
	}
	frame = append(frame, payload...)
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	_, err := c.conn.Write(frame)
	return err
}

// writeJSON writes a JSON-encoded text frame to the connection.
func (c *wsConn) writeJSON(v any) error {
	data, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(opText, data)
}

// close sends a close frame whose status code and reason come from why
// (nil is a normal close), then closes the connection. Later calls do
// nothing.
func (c *wsConn) close(why error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	payload := binary.BigEndian.AppendUint16(nil, closeCode(why))
	if why != nil {
		// A close reason is at most 123 bytes of UTF-8.
		reason := why.Error()
		if len(reason) > 123 {
			reason = strings.ToValidUTF8(reason[:123], "")
		}
		payload = append(payload, reason...)
	}
	// The peer may already be gone; the close frame is a courtesy.
	c.writeFrameLocked(opClose, payload)
	c.conn.Close()
}

func closeCode(why error) uint16 {
	switch {
	case why == nil:
		return closeNormal
	case errors.Is(why, errTooBig):
		return closeTooBig
	case errors.Is(why, errProtocol):
		return closeProtocol
	default:
		return closeInternal
	}
}

// readLoop answers pings, assembles fragmented messages and passes each
// complete message to onMessage until the peer closes, a read fails or
// onMessage fails; then it closes the connection with the matching status.
func (c *wsConn) readLoop(onMessage func([]byte) error) {
	c.close(c.readMessages(onMessage))
}

// readMessages returns nil when the peer closes, else why reading stopped.
func (c *wsConn) readMessages(onMessage func([]byte) error) error {
	var msg []byte
	fragmented := false
	for {
		f, err := c.readFrame()
		if err != nil {
			return err
		}
		switch f.opcode {
		case opPing:
			if err := c.writeFrame(opPong, f.payload); err != nil {
				return err
			}
			continue
		case opPong:
			continue
		case opClose:
			return nil
		case opText, opBinary:
			if fragmented {
				return fmt.Errorf("%w: new message inside a fragmented one", errProtocol)
			}
			msg = f.payload
		case opContinuation:
			if !fragmented {
				return fmt.Errorf("%w: continuation without a message", errProtocol)
			}
			msg = append(msg, f.payload...)
		default:
			return fmt.Errorf("%w: opcode %#x", errProtocol, byte(f.opcode))
		}
		if len(msg) > maxMessage {
			return errTooBig
		}
		fragmented = !f.fin
		if fragmented {
			continue
		}
		if err := onMessage(msg); err != nil {
			return err
		}
	}
}

// wsHub tracks every connected WebSocket client and broadcasts messages
// to all of them.
type wsHub struct {
	mu    sync.Mutex
	conns map[*wsConn]bool
}

func newWSHub() *wsHub {
	return &wsHub{conns: map[*wsConn]bool{}}
}

// join runs first, then registers c, under the hub lock: no broadcast
// reaches c before first's message, and none is missed between them.
func (h *wsHub) join(c *wsConn, first func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := first(); err != nil {
		return err
	}
	h.conns[c] = true
	return nil
}

// remove unregisters a connection from the hub.
func (h *wsHub) remove(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
}

// broadcast sends v to every client. A client that cannot be written to
// within the write timeout is closed and removed. It fails only when v
// cannot be encoded.
func (h *wsHub) broadcast(v any) error {
	data, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if err := c.writeFrame(opText, data); err != nil {
			c.close(err)
			delete(h.conns, c)
		}
	}
	return nil
}
