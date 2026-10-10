package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// These tests exercise the host-side request demultiplexer without spawning a
// subprocess: the demux is driven directly, and call()/Stop() run against an
// in-process pipe pair that stands in for the child's stdin/stdout.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mustResponseLine(t *testing.T, id int64, result string) []byte {
	t.Helper()
	line, err := json.Marshal(protoResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(result)})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return line
}

// ── demux unit tests ────────────────────────────────────────────────────────

func TestProtoDemuxDispatchDeliversToMatchingWaiter(t *testing.T) {
	d := newProtoDemux(quietLogger())
	first := d.register(1)
	second := d.register(2)

	if !d.dispatch(mustResponseLine(t, 2, `{"ok":"second"}`)) {
		t.Fatal("dispatch reported no waiter for a registered id")
	}

	select {
	case resp := <-second:
		if resp.ID != 2 || string(resp.Result) != `{"ok":"second"}` {
			t.Fatalf("waiter 2 received %+v", resp)
		}
	default:
		t.Fatal("waiter 2 got no response")
	}
	select {
	case resp := <-first:
		t.Fatalf("waiter 1 received a response meant for id %d", resp.ID)
	default:
	}
}

func TestProtoDemuxDropsUnknownAndMalformedLines(t *testing.T) {
	d := newProtoDemux(quietLogger())
	waiter := d.register(1)

	if d.dispatch(mustResponseLine(t, 99, `{}`)) {
		t.Fatal("dispatch claimed a waiter for an unknown id")
	}
	if d.dispatch(mustResponseLine(t, 0, `{}`)) {
		t.Fatal("dispatch claimed a waiter for id 0")
	}
	if d.dispatch([]byte("this is not json")) {
		t.Fatal("dispatch claimed a waiter for a malformed line")
	}
	select {
	case resp := <-waiter:
		t.Fatalf("registered waiter received a dropped response: %+v", resp)
	default:
	}
}

func TestProtoDemuxDropsResponseAfterWaiterGaveUp(t *testing.T) {
	d := newProtoDemux(quietLogger())
	waiter := d.register(7)
	d.deregister(7)

	// The waiter is gone: the reader must drop the late response, not block and
	// not panic (the channel is buffered, so a delivery would still be safe).
	if d.dispatch(mustResponseLine(t, 7, `{}`)) {
		t.Fatal("late response was delivered to a deregistered waiter")
	}
	select {
	case resp := <-waiter:
		t.Fatalf("abandoned waiter received a late response: %+v", resp)
	default:
	}

	// Deregistering an id that was never registered, and deregistering twice,
	// must both be harmless.
	d.deregister(7)
	d.deregister(12345)
}

func TestProtoDemuxLateResponseConcurrentWithDeregisterIsSafe(t *testing.T) {
	// Simulates the real race: the reader grabs the channel for an id while the
	// waiter cancels and deregisters. No panic, no blocked reader.
	for i := 0; i < 200; i++ {
		d := newProtoDemux(quietLogger())
		id := int64(i + 1)
		d.register(id)
		line := mustResponseLine(t, id, `{}`)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.dispatch(line)
		}()
		go func() {
			defer wg.Done()
			d.deregister(id)
		}()
		wg.Wait()

		d.mu.Lock()
		_, stillPending := d.pending[id]
		d.mu.Unlock()
		if stillPending {
			t.Fatalf("iteration %d: deregistered id is still pending", i)
		}
	}
}

func TestDefaultCallTimeoutMatchesSDKGuarantee(t *testing.T) {
	// The host fallback must not silently lengthen or shorten the SDK's per-call
	// bound: today an SDK-based extension always answers within its own call
	// timeout, and a hung one is detected on the same bound.
	if defaultCallTimeout != extsdk.DefaultCallTimeout {
		t.Fatalf("defaultCallTimeout = %v, want extsdk.DefaultCallTimeout = %v",
			defaultCallTimeout, extsdk.DefaultCallTimeout)
	}
}

// ── call()/Stop() over an in-process fake extension ─────────────────────────

// fakeExtension wires a ProtoProcess to in-process pipes. The "extension side"
// goroutine reads request lines (as a child reads stdin) and exits when stdin
// closes, which closes stdout — exactly a well-behaved subprocess on Stop.
type fakeExtension struct {
	proc      *ProtoProcess
	transport *protoTransport
	requests  chan protoRequest
	readerOut chan struct{}

	respW *io.PipeWriter
}

type callOutcome struct {
	resp *protoResponse
	err  error
}

func newFakeExtension(t *testing.T) *fakeExtension {
	t.Helper()
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()
	log := quietLogger()
	tr := newProtoTransport(log)

	pp := &ProtoProcess{
		log:       log,
		stdin:     reqW,
		cancel:    func() {},
		done:      make(chan struct{}),
		transport: tr,
	}
	fe := &fakeExtension{
		proc:      pp,
		transport: tr,
		requests:  make(chan protoRequest, 32),
		readerOut: make(chan struct{}),
		respW:     respW,
	}

	// Extension side: decode request lines until stdin EOFs, then close stdout.
	go func() {
		br := bufio.NewReader(reqR)
		for {
			line, err := br.ReadBytes('\n')
			if err == nil && len(bytes.TrimSpace(line)) > 0 {
				var req protoRequest
				if json.Unmarshal(line, &req) == nil {
					fe.requests <- req
				}
			}
			if err != nil {
				_ = respW.Close()
				return
			}
		}
	}()

	// Host side: the real stdout reader, demultiplexing into call waiters.
	go func() {
		defer close(fe.readerOut)
		pp.readLoop(respR, tr)
	}()

	// Emulate the stderr drain goroutine's "process is gone" signal.
	go func() {
		<-fe.readerOut
		close(pp.done)
	}()

	t.Cleanup(func() { _ = pp.Stop() })
	return fe
}

// nextRequest returns the next request line the host wrote.
func (fe *fakeExtension) nextRequest(t *testing.T) protoRequest {
	t.Helper()
	select {
	case req := <-fe.requests:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a request from the host")
		return protoRequest{}
	}
}

// respond answers one request id, as the extension would.
func (fe *fakeExtension) respond(t *testing.T, id int64, result interface{}, rpcErr *protoRPCErr) {
	t.Helper()
	var raw json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		raw = b
	}
	line, err := json.Marshal(protoResponse{JSONRPC: "2.0", ID: id, Result: raw, Error: rpcErr})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := fe.respW.Write(append(line, '\n')); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func (fe *fakeExtension) pendingCount() int {
	fe.transport.demux.mu.Lock()
	defer fe.transport.demux.mu.Unlock()
	return len(fe.transport.demux.pending)
}

func (fe *fakeExtension) startCall(ctx context.Context, method string, params interface{}) <-chan callOutcome {
	out := make(chan callOutcome, 1)
	go func() {
		resp, err := fe.proc.call(ctx, method, params)
		out <- callOutcome{resp: resp, err: err}
	}()
	return out
}

func TestCallConcurrentRequestsBothComplete(t *testing.T) {
	fe := newFakeExtension(t)

	a := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "a"})
	b := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "b"})

	reqA := fe.nextRequest(t)
	reqB := fe.nextRequest(t)
	if reqA.ID == reqB.ID {
		t.Fatalf("host reused request id %d for two in-flight calls", reqA.ID)
	}
	// Both requests are outstanding at the same time: this is the property the
	// old serialising implementation could not provide.
	if got := fe.pendingCount(); got != 2 {
		t.Fatalf("pending calls while both are in flight = %d, want 2", got)
	}

	// Answer out of order: the demux, not arrival order, must route them.
	fe.respond(t, reqB.ID, protoCallToolResult{Success: true, Output: "b"}, nil)
	fe.respond(t, reqA.ID, protoCallToolResult{Success: true, Output: "a"}, nil)

	got := map[string]string{}
	for _, ch := range []<-chan callOutcome{a, b} {
		select {
		case o := <-ch:
			if o.err != nil {
				t.Fatalf("call failed: %v", o.err)
			}
			var cr protoCallToolResult
			if err := json.Unmarshal(o.resp.Result, &cr); err != nil {
				t.Fatalf("parse result: %v", err)
			}
			got[cr.Output] = cr.Output
		case <-time.After(2 * time.Second):
			t.Fatal("a concurrent call never completed")
		}
	}
	if len(got) != 2 {
		t.Fatalf("both calls should have completed, got %v", got)
	}
	if n := fe.pendingCount(); n != 0 {
		t.Fatalf("pending map leaked %d entries after all calls returned", n)
	}
}

func TestCallContextCancelReturnsErrorAndDeregisters(t *testing.T) {
	fe := newFakeExtension(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	outcome := fe.startCall(ctx, "extension.call_tool", protoCallToolParams{Name: "hang"})
	req := fe.nextRequest(t)

	select {
	case o := <-outcome:
		if o.err == nil {
			t.Fatal("cancelled call returned no error")
		}
		if !errors.Is(o.err, context.DeadlineExceeded) {
			t.Fatalf("cancelled call error = %v, want context.DeadlineExceeded", o.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call ignored its context deadline and never returned")
	}

	if n := fe.pendingCount(); n != 0 {
		t.Fatalf("cancelled call left %d pending entries", n)
	}
	// A late response for the abandoned id must be dropped by the reader.
	select {
	case <-fe.readerOut:
		t.Fatal("reader exited unexpectedly")
	default:
	}
	fe.respond(t, req.ID, protoCallToolResult{Success: true, Output: "too late"}, nil)

	// A fresh call still works: the demux survived the cancellation.
	next := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "ok"})
	req2 := fe.nextRequest(t)
	fe.respond(t, req2.ID, protoCallToolResult{Success: true, Output: "ok"}, nil)
	select {
	case o := <-next:
		if o.err != nil {
			t.Fatalf("call after cancellation failed: %v", o.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call after cancellation never completed")
	}
}

func TestCallFailsWhenProcessExits(t *testing.T) {
	fe := newFakeExtension(t)

	first := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "one"})
	second := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "two"})
	fe.nextRequest(t)
	fe.nextRequest(t)

	// Process death: the exit channel closes before anything answers.
	fe.transport.markExited()

	for i, ch := range []<-chan callOutcome{first, second} {
		select {
		case o := <-ch:
			if o.err == nil {
				t.Fatalf("call %d returned no error after process exit", i)
			}
			if !strings.Contains(o.err.Error(), "exited") {
				t.Fatalf("call %d error = %v, want an exit error", i, o.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("call %d was not failed by process exit", i)
		}
	}
	if n := fe.pendingCount(); n != 0 {
		t.Fatalf("process exit left %d pending entries", n)
	}
}

func TestStopDuringCallReturnsPromptly(t *testing.T) {
	fe := newFakeExtension(t)

	outcome := fe.startCall(context.Background(), "extension.call_tool", protoCallToolParams{Name: "hang"})
	fe.nextRequest(t)

	start := time.Now()
	if err := fe.proc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop took %v; it must not wait for a pending call", elapsed)
	}

	select {
	case o := <-outcome:
		if o.err == nil {
			t.Fatal("in-flight call returned no error after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight call is still blocked after Stop")
	}
	select {
	case <-fe.readerOut:
	case <-time.After(time.Second):
		t.Fatal("stdout reader goroutine did not exit after Stop")
	}
	if n := fe.pendingCount(); n != 0 {
		t.Fatalf("Stop left %d pending entries", n)
	}
	// Stop is idempotent.
	if err := fe.proc.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestProtoToolConcurrencySafeFollowsDeclaration(t *testing.T) {
	pt := &protoTool{proc: &ProtoProcess{Manifest: ProtoManifest{Version: "0.0.1"}}}
	if pt.ConcurrencySafe() {
		t.Fatal("a tool that does not declare concurrency safety must report false")
	}
	var iface ConcurrencySafeTool = pt
	if iface.ConcurrencySafe() {
		t.Fatal("interface dispatch lost the declared value")
	}
	pt.def.ConcurrencySafe = true
	if !pt.ConcurrencySafe() {
		t.Fatal("a tool that declares concurrency safety must report true")
	}
}
