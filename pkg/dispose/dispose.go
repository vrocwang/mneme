// Package dispose provides effect-style teardown primitives. A DisposeFunc
// undoes the side effects of one registration; registration points return
// dispose funcs so unload can unwind them uniformly (LIFO).
package dispose

import (
	"fmt"
	"log/slog"
	"sync"
)

// Func undoes a registration's side effects. It must be idempotent and
// concurrency-safe; errors are best-effort and are not surfaced (cleanup
// failures are logged by the callee, not propagated).
type Func func()

// Once wraps fn so repeated calls invoke it at most once.
func Once(fn Func) Func {
	var once sync.Once
	return func() { once.Do(fn) }
}

// Compose combines multiple dispose funcs into one, executing them in reverse
// order (LIFO: last registered is undone first). Each sub-dispose runs even if
// a previous one panics; a panic is logged and the unwind continues. The
// returned func is idempotent.
func Compose(disposes ...Func) Func {
	return Once(func() {
		for i := len(disposes) - 1; i >= 0; i-- {
			runSafely(disposes[i])
		}
	})
}

// runSafely invokes fn, recovering from panics so a broken teardown does not
// abort the remaining unwinds. The panic is logged (not silently swallowed) so
// teardown bugs stay visible.
//
// Note: runtime.Goexit cannot be recovered; a sub-dispose that calls Goexit
// will terminate the calling goroutine. Dispose funcs must not call Goexit.
func runSafely(fn Func) {
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("dispose: panic during teardown (continuing unwind)", "panic", fmt.Sprint(r))
		}
	}()
	fn()
}
