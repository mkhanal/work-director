package serve

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBoardAnswers500WhenTheLedgerFails(t *testing.T) {
	s := newTestServer(t)
	api := httptest.NewServer(s.routes())
	defer api.Close()
	if err := s.ledger.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	for _, path := range []string{"/api/board", "/api/goals"} {
		var body map[string]string
		getJSON(t, api.URL+path, http.StatusInternalServerError, &body)
		if body["error"] == "" {
			t.Fatalf("%s error = empty, want the ledger failure", path)
		}
	}
}

func TestWebSocketClosesWhenTheBoardFails(t *testing.T) {
	s := newTestServer(t)
	addr, _, err := s.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.ledger.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	client := dialWS(t, addr)
	f, err := readFrame(client.conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if f.opcode != opClose || len(f.payload) <= 2 || binary.BigEndian.Uint16(f.payload) != closeInternal {
		t.Fatalf("frame = %d %q, want a 1011 close naming the failure", f.opcode, f.payload)
	}
}
