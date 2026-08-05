package events

import (
	"fmt"
	"sync"
)

// NativeHandler is a typed request/response handler. It receives a typed request
// and returns a typed response or error. Unlike the pub/sub bus, this is
// one-to-one dispatch with zero serialization — callers cast the response to
// their expected type.
type NativeHandler func(req interface{}) (interface{}, error)

// NativeRegistry provides typed in-process request/response dispatch by method
// name, modeled after the Rust core's register_native_global / request_native_global.
type NativeRegistry struct {
	mu       sync.RWMutex
	handlers map[string]NativeHandler
}

// NewNativeRegistry creates an empty native request registry.
func NewNativeRegistry() *NativeRegistry {
	return &NativeRegistry{handlers: make(map[string]NativeHandler)}
}

// Register installs a handler for the given method. Replaces any existing handler.
func (r *NativeRegistry) Register(method string, handler NativeHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = handler
}

// Unregister removes a handler. Safe to call on non-existent methods.
func (r *NativeRegistry) Unregister(method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, method)
}

// Request dispatches a typed request to the registered handler and returns the
// typed response. Returns an error if no handler is registered for the method
// or if the handler itself returns an error.
func (r *NativeRegistry) Request(method string, req interface{}) (resp interface{}, err error) {
	r.mu.RLock()
	handler, ok := r.handlers[method]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("native request: no handler for %q", method)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("native request: panic in handler %q: %v", method, r)
		}
	}()
	return handler(req)
}

// ── Global singleton ──────────────────────────────────────────────────────

var globalNative = NewNativeRegistry()

// RegisterNativeGlobal installs a handler in the global native registry.
func RegisterNativeGlobal(method string, handler NativeHandler) {
	globalNative.Register(method, handler)
}

// RequestNativeGlobal dispatches a request through the global native registry.
func RequestNativeGlobal(method string, req interface{}) (interface{}, error) {
	return globalNative.Request(method, req)
}

// NativeRegistry returns the global native registry for direct access.
func NativeRegistryGlobal() *NativeRegistry {
	return globalNative
}
