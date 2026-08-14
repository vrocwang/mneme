package dispose

import (
	"sync"
	"testing"
)

func TestOnce_Idempotent(t *testing.T) {
	calls := 0
	d := Once(func() { calls++ })
	d()
	d()
	d()
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestCompose_LIFOAndIdempotent(t *testing.T) {
	var order []int
	mk := func(n int) Func { return func() { order = append(order, n) } }
	d := Compose(mk(1), mk(2), mk(3))

	d()
	d() // second call must be a no-op

	if len(order) != 3 {
		t.Fatalf("expected 3 unwinds, got %d: %v", len(order), order)
	}
	// LIFO: last registered (3) unwound first.
	want := []int{3, 2, 1}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, order)
		}
	}
}

func TestCompose_ContinuesOnPanic(t *testing.T) {
	calls := 0
	d := Compose(
		func() { calls++ }, // runs last (LIFO)
		func() { panic("boom") },
		func() { calls++ }, // runs first
	)
	d() // must not propagate the panic
	if calls != 2 {
		t.Fatalf("expected 2 non-panicking unwinds to run, got %d", calls)
	}
}

func TestOnce_Concurrent(t *testing.T) {
	calls := 0
	var mu sync.Mutex
	d := Once(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d() }()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 call under concurrency, got %d", calls)
	}
}
