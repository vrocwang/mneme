package events

import (
	"testing"
)

type searchReq struct {
	Query string
	Limit int
}
type searchResp struct {
	Results []string
}

func TestNativeRegistry_RegisterAndRequest(t *testing.T) {
	r := NewNativeRegistry()
	r.Register("memory.search", func(req interface{}) (interface{}, error) {
		sr := req.(searchReq)
		return &searchResp{Results: []string{"result for: " + sr.Query}}, nil
	})

	resp, err := r.Request("memory.search", searchReq{Query: "test", Limit: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sr := resp.(*searchResp)
	if len(sr.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(sr.Results))
	}
}

func TestNativeRegistry_UnknownMethod(t *testing.T) {
	r := NewNativeRegistry()
	_, err := r.Request("nonexistent", nil)
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestNativeRegistry_Unregister(t *testing.T) {
	r := NewNativeRegistry()
	r.Register("test.method", func(req interface{}) (interface{}, error) { return "ok", nil })
	r.Unregister("test.method")
	_, err := r.Request("test.method", nil)
	if err == nil {
		t.Error("expected error after unregister")
	}
}

func TestNativeRegistry_Global(t *testing.T) {
	RegisterNativeGlobal("global.test", func(req interface{}) (interface{}, error) {
		return req.(string) + "-response", nil
	})
	defer globalNative.Unregister("global.test")

	resp, err := RequestNativeGlobal("global.test", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "hello-response" {
		t.Errorf("expected 'hello-response', got %v", resp)
	}
}

func TestNativeRegistry_HandlerError(t *testing.T) {
	r := NewNativeRegistry()
	r.Register("failing.method", func(req interface{}) (interface{}, error) {
		return nil, errTest
	})
	_, err := r.Request("failing.method", nil)
	if err != errTest {
		t.Error("expected handler error to propagate")
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
