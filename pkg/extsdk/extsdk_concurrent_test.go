package extsdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests drive Server.run over in-process pipes (the same loop that Run
// wraps around os.Stdin/os.Stdout), so they do not need a subprocess.

type testHarness struct {
	server *Server
	reqW   *io.PipeWriter
	runErr chan error
	resps  chan rpcResponse
}

func startHarness(t *testing.T, s *Server) *testHarness {
	t.Helper()
	s.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()
	h := &testHarness{
		server: s,
		reqW:   reqW,
		runErr: make(chan error, 1),
		resps:  make(chan rpcResponse, 64),
	}

	go func() {
		err := s.run(reqR, respW)
		_ = respW.Close()
		h.runErr <- err
	}()
	go func() {
		br := bufio.NewReader(respR)
		for {
			line, err := br.ReadBytes('\n')
			if err == nil && len(bytes.TrimSpace(line)) > 0 {
				var resp rpcResponse
				if json.Unmarshal(line, &resp) == nil {
					h.resps <- resp
				}
			}
			if err != nil {
				close(h.resps)
				return
			}
		}
	}()
	return h
}

// writeCall writes one extension.call_tool request line. The write happens in
// a goroutine because io.Pipe is unbuffered: a real host writes into the
// kernel pipe buffer and does not block behind an extension that is busy
// handling an earlier request (which is exactly the sequential case).
func (h *testHarness) writeCall(t *testing.T, id int64, tool string) {
	t.Helper()
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "extension.call_tool",
		Params:  json.RawMessage(fmt.Sprintf(`{"name":%q,"args":{}}`, tool)),
	}
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	go func() {
		// A write error only means the harness closed stdin on shutdown.
		_, _ = h.reqW.Write(append(line, '\n'))
	}()
}

// nextResponse returns the next response, or ok=false if none arrived in time.
func (h *testHarness) nextResponse(t *testing.T, wait time.Duration) (rpcResponse, bool) {
	t.Helper()
	select {
	case resp, ok := <-h.resps:
		return resp, ok
	case <-time.After(wait):
		return rpcResponse{}, false
	}
}

func (h *testHarness) awaitRun(t *testing.T, wait time.Duration) error {
	t.Helper()
	select {
	case err := <-h.runErr:
		return err
	case <-time.After(wait):
		t.Fatalf("Server.run did not return within %v", wait)
		return nil
	}
}

// blockingToolServer registers "slow" (blocks until released) and "fast".
func blockingToolServer(t *testing.T, concurrent bool) (*Server, <-chan struct{}, chan struct{}) {
	t.Helper()
	s := NewServer(Manifest{Name: "concurrent-test", Version: "0.1.0"})

	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	var startedOnce sync.Once

	s.RegisterTool(ToolDef{Name: "slow"}, func(ctx context.Context, args map[string]interface{}) Result {
		startedOnce.Do(func() { close(slowStarted) })
		<-releaseSlow
		return Result{Success: true, Output: "slow"}
	})
	s.RegisterTool(ToolDef{Name: "fast"}, func(ctx context.Context, args map[string]interface{}) Result {
		return Result{Success: true, Output: "fast"}
	})
	s.SetConcurrentDispatch(concurrent)
	return s, slowStarted, releaseSlow
}

func TestConcurrentDispatchServesSecondCallWhileFirstRuns(t *testing.T) {
	s, slowStarted, releaseSlow := blockingToolServer(t, true)
	h := startHarness(t, s)

	h.writeCall(t, 1, "slow")
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}

	// The second call must be read and answered while "slow" is still running.
	h.writeCall(t, 2, "fast")
	resp, ok := h.nextResponse(t, 2*time.Second)
	if !ok {
		t.Fatal("no response while the first call was still in flight: calls are not dispatched concurrently")
	}
	if resp.ID != 2 {
		t.Fatalf("first response was for id %d, want 2 (the fast call)", resp.ID)
	}
	var res Result
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("parse fast result: %v", err)
	}
	if res.Output != "fast" {
		t.Fatalf("fast output = %q", res.Output)
	}

	close(releaseSlow)
	resp, ok = h.nextResponse(t, 2*time.Second)
	if !ok || resp.ID != 1 {
		t.Fatalf("slow response = %+v (ok=%v), want id 1", resp, ok)
	}

	_ = h.reqW.Close()
	if err := h.awaitRun(t, 2*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestSequentialDispatchIsStillTheDefault(t *testing.T) {
	s, slowStarted, releaseSlow := blockingToolServer(t, false)
	if s.concurrent.Load() {
		t.Fatal("concurrent dispatch must default to off")
	}
	h := startHarness(t, s)

	h.writeCall(t, 1, "slow")
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}
	h.writeCall(t, 2, "fast")

	// With dispatch off the loop is still inside the first handler, so nothing
	// can be answered yet: this is the behaviour every existing extension keeps.
	if resp, ok := h.nextResponse(t, 300*time.Millisecond); ok {
		t.Fatalf("got a response for id %d before the first call finished", resp.ID)
	}

	close(releaseSlow)
	first, ok := h.nextResponse(t, 2*time.Second)
	if !ok || first.ID != 1 {
		t.Fatalf("first response = %+v (ok=%v), want id 1", first, ok)
	}
	second, ok := h.nextResponse(t, 2*time.Second)
	if !ok || second.ID != 2 {
		t.Fatalf("second response = %+v (ok=%v), want id 2", second, ok)
	}

	_ = h.reqW.Close()
	if err := h.awaitRun(t, 2*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestConcurrentDispatchWaitsForInFlightOnEOF(t *testing.T) {
	s, slowStarted, releaseSlow := blockingToolServer(t, true)
	h := startHarness(t, s)

	h.writeCall(t, 1, "slow")
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}

	// stdin EOF while the handler is still running: run must not return (and
	// must not drop the response) until the call is done.
	if err := h.reqW.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	select {
	case err := <-h.runErr:
		t.Fatalf("run returned (%v) before the in-flight call finished", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(releaseSlow)
	resp, ok := h.nextResponse(t, 2*time.Second)
	if !ok || resp.ID != 1 {
		t.Fatalf("in-flight response = %+v (ok=%v), want id 1", resp, ok)
	}
	if err := h.awaitRun(t, 2*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestConcurrentDispatchFramesResponsesOneLineEach(t *testing.T) {
	s := NewServer(Manifest{Name: "framing-test", Version: "0.1.0"})
	const payload = 4096
	s.RegisterTool(ToolDef{Name: "echo"}, func(ctx context.Context, args map[string]interface{}) Result {
		id, _ := args["id"].(string)
		return Result{Success: true, Output: id + ":" + strings.Repeat("x", payload)}
	})
	s.SetConcurrentDispatch(true)
	h := startHarness(t, s)

	const calls = 8
	for i := 0; i < calls; i++ {
		req := rpcRequest{
			JSONRPC: "2.0",
			ID:      int64(i + 1),
			Method:  "extension.call_tool",
			Params:  json.RawMessage(fmt.Sprintf(`{"name":"echo","args":{"id":"%d"}}`, i+1)),
		}
		line, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := h.reqW.Write(append(line, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	seen := map[int64]bool{}
	for i := 0; i < calls; i++ {
		resp, ok := h.nextResponse(t, 2*time.Second)
		if !ok {
			t.Fatalf("only %d of %d concurrent responses arrived", i, calls)
		}
		var res Result
		if err := json.Unmarshal(resp.Result, &res); err != nil {
			t.Fatalf("response %d was not a whole JSON line: %v", resp.ID, err)
		}
		want := fmt.Sprintf("%d:%s", resp.ID, strings.Repeat("x", payload))
		if res.Output != want {
			t.Fatalf("response %d payload was corrupted (len=%d)", resp.ID, len(res.Output))
		}
		seen[resp.ID] = true
	}
	if len(seen) != calls {
		t.Fatalf("got %d distinct ids, want %d", len(seen), calls)
	}

	_ = h.reqW.Close()
	if err := h.awaitRun(t, 2*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestConcurrentDispatchToolDefRoundTrips guards the wire contract the host
// relies on for ConcurrencySafeTool.
func TestConcurrentDispatchToolDefRoundTrips(t *testing.T) {
	s := NewServer(Manifest{Name: "defs", Version: "0.1.0"})
	s.RegisterTool(ToolDef{Name: "safe", ConcurrencySafe: true}, func(context.Context, map[string]interface{}) Result {
		return Result{Success: true}
	})
	s.RegisterTool(ToolDef{Name: "unsafe"}, func(context.Context, map[string]interface{}) Result {
		return Result{Success: true}
	})

	raw, err := json.Marshal(s.tools)
	if err != nil {
		t.Fatalf("marshal tool defs: %v", err)
	}
	if !strings.Contains(string(raw), `"concurrency_safe":true`) {
		t.Fatalf("declared tool lost its concurrency_safe flag: %s", raw)
	}
	if strings.Contains(string(raw), `"name":"unsafe"`) && strings.Count(string(raw), "concurrency_safe") != 1 {
		t.Fatalf("omitempty not honoured for undeclared tools: %s", raw)
	}
}
