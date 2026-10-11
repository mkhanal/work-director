package serve

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"wd/internal/core"
)

// stdioRequest is one line a client writes.
type stdioRequest struct {
	ID     *int64 `json:"id"`
	Method string `json:"method"`
	Params struct {
		ID   string   `json:"id"`
		Argv []string `json:"argv"`
		From int      `json:"from"`
	} `json:"params"`
}

type stdioError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type stdioReply struct {
	ID     int64       `json:"id"`
	Result any         `json:"result,omitempty"`
	Error  *stdioError `json:"error,omitempty"`
}

// stdioUnaddressed answers a line that carried no request id to answer to.
type stdioUnaddressed struct {
	Error stdioError `json:"error"`
}

type stdioPush struct {
	Push string     `json:"push"`
	Data core.Event `json:"data"`
}

// maxLine bounds one request line; an argv longer than this is not a request.
const maxLine = 1 << 20

// ServeStdio answers requests read from in, one JSON object per line, and writes
// replies and pushed ledger events to out, one per line. Requests run
// concurrently, because an action can be a whole `wd drive`; each reply carries
// its request's id. It returns nil once in ends and every request has been
// answered.
func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	last, err := s.ledger.LastEventID()
	if err != nil {
		return err
	}
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	write := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(v)
	}

	stop := make(chan struct{})
	followed := make(chan error, 1)
	go func() {
		followed <- s.followEvents(last, stop, func(e core.Event) error {
			return write(stdioPush{Push: "event", Data: e})
		})
	}()

	watched := make(chan error, 1)
	go func() { watched <- s.watchQuestions(stop) }()

	var pending sync.WaitGroup
	failed := make(chan error, 1)
	fail := func(err error) {
		select {
		case failed <- err:
		default:
		}
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		var req stdioRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil || req.ID == nil {
			msg := "a request is {\"id\": n, \"method\": m, \"params\": {...}}"
			if err != nil {
				msg = "invalid request: " + err.Error()
			}
			if err := write(stdioUnaddressed{Error: stdioError{Kind: "bad-request", Message: msg}}); err != nil {
				fail(err)
				break
			}
			continue
		}
		pending.Add(1)
		go func() {
			defer pending.Done()
			if err := write(s.answer(req)); err != nil {
				fail(err)
			}
		}()
	}
	pending.Wait()
	close(stop)
	if err := <-followed; err != nil {
		return err
	}
	if err := <-watched; err != nil {
		return err
	}
	select {
	case err := <-failed:
		return fmt.Errorf("write reply: %w", err)
	default:
	}
	return sc.Err()
}

// answer runs one request against the views every transport shares.
func (s *Server) answer(req stdioRequest) stdioReply {
	var result any
	var err error
	switch req.Method {
	case "board":
		result, err = s.board()
	case "goal":
		if req.Params.ID == "" {
			err = badRequest{"goal needs params.id"}
			break
		}
		result, err = s.goal(req.Params.ID)
	case "work":
		result, err = s.work(req.Params.ID)
	case "events":
		result, err = s.events(req.Params.ID)
	case "conversation":
		result, err = s.conversation(req.Params.ID, req.Params.From)
	case "action":
		if len(req.Params.Argv) == 0 {
			err = badRequest{"action needs params.argv"}
			break
		}
		result, err = s.runCLI(req.Params.Argv)
	default:
		err = badRequest{fmt.Sprintf("unknown method %q: one of board, goal, work, events, conversation, action", req.Method)}
	}
	if err != nil {
		return stdioReply{ID: *req.ID, Error: &stdioError{Kind: errorKind(err), Message: err.Error()}}
	}
	return stdioReply{ID: *req.ID, Result: result}
}
