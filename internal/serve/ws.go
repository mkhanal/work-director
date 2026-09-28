// Package serve is the loopback HTTP and WebSocket adapter over the core
// and ledger. It is a port of the TypeScript wd ui (packages/wd/src/ui.ts):
// same JSON endpoints, same board data, same action dispatch. It binds to
// 127.0.0.1 only — a local adapter for native clients, not a remote server.
package serve

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// wsGUID is the WebSocket protocol GUID from RFC 6455.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

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

// wsConn is one WebSocket connection: the underlying connection, a writer
// lock (WebSocket frames are written from multiple goroutines), and a
// closed flag.
type wsConn struct {
	conn   net.Conn
	wmu    sync.Mutex
	closed bool
}

// wsUpgrade performs the WebSocket handshake on an HTTP connection and
// returns the upgraded connection. It fails when the request is not a
// valid WebSocket upgrade.
func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if r.Header.Get("Upgrade") != "websocket" {
		return nil, errors.New("not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("connection not hijackable")
	}
	nc, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	accept := wsAccept(key)
	_, err = fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		nc.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		nc.Close()
		return nil, err
	}
	return &wsConn{conn: nc}, nil
}

// wsAccept computes the Sec-WebSocket-Accept value from the client key.
func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readFrame reads one WebSocket frame from the connection. It returns
// io.EOF when the client closes the connection cleanly.
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

// writeFrame writes one WebSocket frame to the connection. Server frames
// are never masked.
func (c *wsConn) writeFrame(opcode wsOpCode, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	var hdr []byte
	hdr = append(hdr, byte(0x80|opcode))
	switch {
	case len(payload) < 126:
		hdr = append(hdr, byte(len(payload)))
	case len(payload) < 65536:
		hdr = append(hdr, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		hdr = append(hdr, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		hdr = append(hdr, ext[:]...)
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// writeText writes a text frame to the connection.
func (c *wsConn) writeText(text string) error {
	return c.writeFrame(opText, []byte(text))
}

// writeJSON writes a JSON-encoded text frame to the connection.
func (c *wsConn) writeJSON(v any) error {
	data, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(opText, data)
}

// close sends a close frame and marks the connection closed.
func (c *wsConn) close() {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.writeFrame(opClose, nil)
	c.conn.Close()
}

// readLoop reads frames from the connection, dispatching control frames
// and passing message frames to the handler. It returns when the
// connection closes or an error occurs.
func (c *wsConn) readLoop(onMessage func([]byte) error) {
	for {
		frame, err := c.readFrame()
		if err != nil {
			return
		}
		switch frame.opcode {
		case opPing:
			if err := c.writeFrame(opPong, frame.payload); err != nil {
				return
			}
		case opPong:
			// ignore
		case opClose:
			c.close()
			return
		case opText, opBinary:
			if err := onMessage(frame.payload); err != nil {
				return
			}
		}
	}
}

// wsHub tracks every connected WebSocket client and broadcasts messages
// to all of them.
type wsHub struct {
	mu   sync.Mutex
	conns map[*wsConn]bool
}

func newWSHub() *wsHub {
	return &wsHub{conns: map[*wsConn]bool{}}
}

// add registers a connection with the hub.
func (h *wsHub) add(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = true
}

// remove unregisters a connection from the hub.
func (h *wsHub) remove(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
}

// broadcast sends a JSON message to every connected client.
func (h *wsHub) broadcast(v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		c.writeJSON(v)
	}
}

// count returns the number of connected clients.
func (h *wsHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// bufioReadWriter pairs a buffered reader and writer for the WebSocket
// handshake.
type bufioReadWriter struct {
	*bufio.Reader
	*bufio.Writer
}
