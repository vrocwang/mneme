// Package dispose provides effect-style teardown primitives. A DisposeFunc
// undoes the side effects of one registration; registration points return
// dispose funcs so unload can unwind them uniformly (LIFO).
package dispose

import "sync"

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
// a previous one panics; a panic in a sub-dispose is recovered and swallowed
// so cleanup continues. The returned func is idempotent.
func Compose(disposes ...Func) Func {
	return Once(func() {
		for i := len(disposes) - 1; i >= 0; i-- {
			runSafely(disposes[i])
		}
	})
}

// runSafely invokes fn, recovering from panics so a broken teardown does not
// abort the remaining unwinds.
func runSafely(fn Func) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn()
}
