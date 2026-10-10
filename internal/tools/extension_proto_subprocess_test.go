package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeExtensionScript is a minimal JSON-RPC extension used to exercise the real
// StartProto wiring (subprocess, pipes, demultiplexing reader). "slow" sleeps
// before answering while "cancel" answers immediately, so the host must be able
// to have two requests in flight and route two out-of-order responses.
const fakeExtensionScript = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  name=$(printf '%s' "$line" | sed -n 's/.*"name":"\([^"]*\)".*/\1/p')
  case "$method" in
    extension.describe|manifest)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"name":"fake-ext","version":"0.0.1","tools":["slow","cancel"],"agent_defs":[],"protocol_min":1}}\n' "$id"
      ;;
    extension.list_tools)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}\n' "$id"
      ;;
    extension.call_tool)
      if [ "$name" = "slow" ]; then
        ( sleep 1; printf '{"jsonrpc":"2.0","id":%s,"result":{"success":true,"output":"slow"}}\n' "$id" ) 2>/dev/null &
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"success":true,"output":"cancel"}}\n' "$id"
      fi
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}\n' "$id"
      ;;
  esac
done
`

// TestStartProtoDemultiplexesRealSubprocess starts a real (shell-script)
// extension through the public entry point and proves the host can keep two
// requests in flight: the fast "cancel" call returns while the slow call is
// still outstanding, and the slow response is then routed to its own waiter.
func TestStartProtoDemultiplexesRealSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ext.sh")
	if err := os.WriteFile(script, []byte(fakeExtensionScript), 0o644); err != nil {
		t.Fatalf("write fake extension: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// "sh <script>" (the interpreter form) rather than the bare script path:
	// the bare-path branch of StartProtoFromCommand sniffs the interpreter from
	// the shebang, and resolveInterpreter reads a 128-byte block without stopping
	// at the first newline (it splits the whole body into interpreter args), so
	// script-only extensions cannot be launched that way. That is a pre-existing
	// bug outside this change's scope; the interpreter form exercises the same
	// StartProto wiring.
	proc, err := StartProtoFromCommand(ctx, "sh "+script, quietLogger())
	if err != nil {
		t.Fatalf("StartProtoFromCommand: %v", err)
	}
	defer proc.Stop()
	if proc.Manifest.Name != "fake-ext" {
		t.Fatalf("handshake manifest name = %q, want fake-ext", proc.Manifest.Name)
	}

	callTool := func(name string) (string, time.Duration, error) {
		start := time.Now()
		resp, err := proc.call(ctx, "extension.call_tool", protoCallToolParams{Name: name})
		if err != nil {
			return "", time.Since(start), err
		}
		var cr protoCallToolResult
		if err := json.Unmarshal(resp.Result, &cr); err != nil {
			return "", time.Since(start), err
		}
		return cr.Output, time.Since(start), nil
	}

	type result struct {
		output  string
		elapsed time.Duration
		err     error
	}
	slowCh := make(chan result, 1)
	go func() {
		out, elapsed, err := callTool("slow")
		slowCh <- result{out, elapsed, err}
	}()

	// The cancel call is written while "slow" is still in flight. Under the old
	// serialising transport this could not even be sent.
	out, cancelElapsed, err := callTool("cancel")
	if err != nil {
		t.Fatalf("cancel call failed: %v", err)
	}
	if out != "cancel" {
		t.Fatalf("cancel output = %q, want cancel", out)
	}
	if cancelElapsed > 800*time.Millisecond {
		t.Fatalf("cancel call took %v; it should not wait behind the slow call", cancelElapsed)
	}

	select {
	case r := <-slowCh:
		if r.err != nil {
			t.Fatalf("slow call failed: %v", r.err)
		}
		if r.output != "slow" {
			t.Fatalf("slow output = %q, want slow", r.output)
		}
		if r.elapsed < 900*time.Millisecond {
			t.Fatalf("slow call returned in %v; the fixture should have slept ~1s", r.elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow call never completed")
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if proc.IsAlive() {
		t.Fatal("extension still alive after Stop")
	}
}

// TestStartProtoFailsPendingCallsOnStop starts the fake extension, leaves a
// slow call in flight, and asserts Stop fails it promptly rather than blocking
// the host goroutine on a dead process.
func TestStartProtoFailsPendingCallsOnStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ext.sh")
	if err := os.WriteFile(script, []byte(fakeExtensionScript), 0o644); err != nil {
		t.Fatalf("write fake extension: %v", err)
	}

	proc, err := StartProtoFromCommand(context.Background(), "sh "+script, quietLogger())
	if err != nil {
		t.Fatalf("StartProtoFromCommand: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := proc.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "slow"})
		errCh <- err
	}()
	// Give the request time to reach the subprocess.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	_ = proc.Stop()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop took %v with a call in flight", elapsed)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("in-flight call returned no error after Stop")
		}
		if !strings.Contains(err.Error(), "exited") {
			t.Fatalf("in-flight call error = %v, want an exit error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight call stayed blocked after Stop")
	}
}
