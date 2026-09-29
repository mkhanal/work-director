package serve

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// clientFrame encodes one unmasked frame as a client would send it.
func clientFrame(fin bool, op wsOpCode, payload []byte) []byte {
	b0 := byte(op)
	if fin {
		b0 |= 0x80
	}
	frame := []byte{b0}
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
	return append(frame, payload...)
}

// pipeConn returns a server connection writing within writeTimeout and the
// raw peer end, with a deadline on the peer.
func pipeConn(t *testing.T, writeTimeout time.Duration) (*wsConn, net.Conn) {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { peer.Close(); server.Close() })
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	return newWSConn(server, writeTimeout), peer
}

func send(t *testing.T, peer net.Conn, frame []byte) {
	t.Helper()
	if _, err := peer.Write(frame); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// requireClose reads the next frame from peer and requires a close with code.
func requireClose(t *testing.T, peer net.Conn, code uint16) {
	t.Helper()
	f, err := readFrame(peer)
	if err != nil {
		t.Fatalf("read close: %v", err)
	}
	if f.opcode != opClose || len(f.payload) < 2 {
		t.Fatalf("frame = %+v, want a close frame", f)
	}
	if got := binary.BigEndian.Uint16(f.payload); got != code {
		t.Fatalf("close code = %d, want %d (reason %q)", got, code, f.payload[2:])
	}
}

func TestReadLoopAssemblesFragmentedMessages(t *testing.T) {
	c, peer := pipeConn(t, wsWriteTimeout)
	got := make(chan string, 1)
	go c.readLoop(func(p []byte) error { got <- string(p); return nil })

	send(t, peer, clientFrame(false, opText, []byte(`{"type":`)))
	send(t, peer, clientFrame(true, opPing, []byte("hi")))
	pong, err := readFrame(peer)
	if err != nil || pong.opcode != opPong || string(pong.payload) != "hi" {
		t.Fatalf("pong = %+v, %v; want a pong echoing hi", pong, err)
	}
	send(t, peer, clientFrame(false, opContinuation, []byte(`"act`)))
	send(t, peer, clientFrame(true, opContinuation, []byte(`ion"}`)))
	select {
	case msg := <-got:
		if msg != `{"type":"action"}` {
			t.Fatalf("message = %q, want the fragments joined", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message delivered")
	}
}

func TestReadLoopRefusesAnOversizedFrame(t *testing.T) {
	c, peer := pipeConn(t, wsWriteTimeout)
	go c.readLoop(func([]byte) error { t.Error("oversized frame delivered"); return nil })
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], 1<<40)
	send(t, peer, append([]byte{0x80 | byte(opText), 127}, ext[:]...))
	requireClose(t, peer, closeTooBig)
}

func TestReadLoopRefusesAnOversizedFragmentedMessage(t *testing.T) {
	c, peer := pipeConn(t, wsWriteTimeout)
	go c.readLoop(func([]byte) error { t.Error("oversized message delivered"); return nil })
	half := make([]byte, maxMessage/2+1)
	send(t, peer, clientFrame(false, opText, half))
	send(t, peer, clientFrame(true, opContinuation, half))
	requireClose(t, peer, closeTooBig)
}

func TestBroadcastDoesNotWaitOnAStalledClient(t *testing.T) {
	h := newWSHub()
	// The stalled client's peer end is never read.
	stalled, stalledPeer := pipeConn(t, 10*time.Second)
	healthy, healthyPeer := pipeConn(t, wsWriteTimeout)
	for _, c := range []*wsConn{stalled, healthy} {
		if err := h.join(c, func() error { return nil }); err != nil {
			t.Fatalf("join: %v", err)
		}
	}

	// Past a full queue of frames, the stalled client is dropped.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range wsQueue + 2 {
			if err := h.broadcast(wsError{Type: "error", Error: strconv.Itoa(i)}); err != nil {
				t.Errorf("broadcast: %v", err)
				return
			}
			f, err := readFrame(healthyPeer)
			if err != nil {
				t.Errorf("healthy read: %v", err)
				return
			}
			if want := fmt.Sprintf(`{"type":"error","error":"%d"}`, i); string(f.payload) != want {
				t.Errorf("healthy client got %q, want %q", f.payload, want)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("broadcast blocked on a stalled client")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[stalled] {
		t.Fatal("stalled client still in the hub")
	}
	if !h.conns[healthy] {
		t.Fatal("healthy client dropped from the hub")
	}
	// Dropped, not closed: the connection ends with no close frame.
	if f, err := readFrame(stalledPeer); !errors.Is(err, io.EOF) {
		t.Fatalf("stalled peer read = %+v, %v; want EOF", f, err)
	}
}

func TestJoinDoesNotWaitOnASlowJoiner(t *testing.T) {
	h := newWSHub()
	// The joiner's peer end is read only after join and a broadcast return.
	slow, slowPeer := pipeConn(t, 10*time.Second)
	done := make(chan error, 1)
	go func() {
		err := h.join(slow, func() error { return slow.writeJSON(wsError{Type: "error", Error: "board"}) })
		if err == nil {
			err = h.broadcast(wsError{Type: "error", Error: "event"})
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("join or broadcast: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("join held the hub on a slow joiner")
	}
	for _, want := range []string{"board", "event"} {
		f, err := readFrame(slowPeer)
		if err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if w := fmt.Sprintf(`{"type":"error","error":"%s"}`, want); string(f.payload) != w {
			t.Fatalf("joiner got %q, want %q", f.payload, w)
		}
	}
}
