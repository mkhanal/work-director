package serve

import (
	"encoding/binary"
	"net"
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

// pipeConn returns a server connection and the raw peer end, with a deadline on the peer.
func pipeConn(t *testing.T) (*wsConn, net.Conn) {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { peer.Close(); server.Close() })
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	return newWSConn(server), peer
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
	f, err := newWSConn(peer).readFrame()
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
	c, peer := pipeConn(t)
	got := make(chan string, 1)
	go c.readLoop(func(p []byte) error { got <- string(p); return nil })

	send(t, peer, clientFrame(false, opText, []byte(`{"type":`)))
	send(t, peer, clientFrame(true, opPing, []byte("hi")))
	pong, err := newWSConn(peer).readFrame()
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
	c, peer := pipeConn(t)
	go c.readLoop(func([]byte) error { t.Error("oversized frame delivered"); return nil })
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], 1<<40)
	send(t, peer, append([]byte{0x80 | byte(opText), 127}, ext[:]...))
	requireClose(t, peer, closeTooBig)
}

func TestReadLoopRefusesAnOversizedFragmentedMessage(t *testing.T) {
	c, peer := pipeConn(t)
	go c.readLoop(func([]byte) error { t.Error("oversized message delivered"); return nil })
	half := make([]byte, maxMessage/2+1)
	send(t, peer, clientFrame(false, opText, half))
	send(t, peer, clientFrame(true, opContinuation, half))
	requireClose(t, peer, closeTooBig)
}

func TestBroadcastClosesAStalledClient(t *testing.T) {
	h := newWSHub()
	// The stalled client's peer end is never read.
	stalled, _ := pipeConn(t)
	stalled.writeTimeout = 50 * time.Millisecond
	healthy, healthyPeer := pipeConn(t)
	for _, c := range []*wsConn{stalled, healthy} {
		if err := h.join(c, func() error { return nil }); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	got := make(chan wsFrame, 1)
	go func() {
		f, err := newWSConn(healthyPeer).readFrame()
		if err != nil {
			t.Errorf("healthy read: %v", err)
		}
		got <- f
	}()

	done := make(chan error, 1)
	go func() { done <- h.broadcast(wsError{Type: "error", Error: "ping"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("broadcast: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broadcast blocked on a stalled client")
	}
	if f := <-got; string(f.payload) != `{"type":"error","error":"ping"}` {
		t.Fatalf("healthy client got %q", f.payload)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[stalled] {
		t.Fatal("stalled client still in the hub")
	}
	if !h.conns[healthy] {
		t.Fatal("healthy client dropped from the hub")
	}
}
