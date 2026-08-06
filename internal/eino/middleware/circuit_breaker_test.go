package middleware

import "testing"

func TestRecordFailure_RepeatThreshold(t *testing.T) {
	c := NewCircuitBreaker()
	// Same tool+args failing 3 times trips the repeat-failure breaker.
	for i := 1; i <= 2; i++ {
		if c.RecordFailure("shell", `{"command":"bad"}`) {
			t.Fatalf("should not trip before threshold (call %d)", i)
		}
	}
	if !c.RecordFailure("shell", `{"command":"bad"}`) {
		t.Fatal("expected trip on 3rd repeat failure")
	}
	if !c.IsTripped() {
		t.Fatal("breaker should be tripped")
	}
	if c.Reason() == "" {
		t.Error("tripped breaker should report a reason")
	}
}

func TestRecordFailure_ConsecutiveThreshold(t *testing.T) {
	c := NewCircuitBreaker()
	// 6 distinct tool+args failures trip the consecutive-failure breaker
	// (each key only fails once, so the repeat threshold never triggers).
	for i := 0; i < 5; i++ {
		if c.RecordFailure("tool", string(rune('a'+i))) {
			t.Fatalf("should not trip before 6th consecutive failure (call %d)", i+1)
		}
	}
	if !c.RecordFailure("tool", "f") {
		t.Fatal("expected trip on 6th consecutive failure")
	}
}

func TestRecordFailure_DistinctArgsNoRepeatTrip(t *testing.T) {
	c := NewCircuitBreaker()
	// Two failures with different args do not hit the repeat threshold (3).
	c.RecordFailure("shell", `{"command":"a"}`)
	c.RecordFailure("shell", `{"command":"b"}`)
	if c.IsTripped() {
		t.Fatal("two distinct-arg failures must not trip the repeat breaker")
	}
}

func TestRecordSuccess_ResetsConsecutive(t *testing.T) {
	c := NewCircuitBreaker()
	for i := 0; i < 5; i++ {
		c.RecordFailure("tool", string(rune('a'+i)))
	}
	c.RecordSuccess() // resets consecutive counter
	// One more failure should not trip (consecutive back to 1).
	if c.RecordFailure("tool", "z") {
		t.Fatal("success should reset the consecutive-failure counter")
	}
	if c.IsTripped() {
		t.Fatal("breaker should remain armed after success + 1 failure")
	}
}

func TestRecordOutput_NarrationLoop(t *testing.T) {
	c := NewCircuitBreaker()
	out := "I am thinking about this."
	for i := 1; i <= 3; i++ {
		if c.RecordOutput(out) {
			t.Fatalf("should not trip before 4th identical output (call %d)", i)
		}
	}
	if !c.RecordOutput(out) {
		t.Fatal("expected trip on 4th identical output (narration loop)")
	}
}

func TestRecordOutput_DifferentResetsCount(t *testing.T) {
	c := NewCircuitBreaker()
	c.RecordOutput("a")
	c.RecordOutput("a")
	c.RecordOutput("a")
	// A different output resets the streak.
	c.RecordOutput("b")
	c.RecordOutput("b")
	c.RecordOutput("b")
	if c.IsTripped() {
		t.Fatal("interrupted streak should not trip the narration breaker")
	}
}

func TestReset_ClearsState(t *testing.T) {
	c := NewCircuitBreaker()
	for i := 0; i < 3; i++ {
		c.RecordFailure("shell", `{"command":"bad"}`)
	}
	if !c.IsTripped() {
		t.Fatal("precondition: breaker should be tripped")
	}
	c.Reset()
	if c.IsTripped() {
		t.Fatal("Reset should clear the tripped state")
	}
	if c.Reason() != "" {
		t.Error("Reset should clear the trip reason")
	}
	// After reset the breaker can trip again.
	for i := 0; i < 3; i++ {
		c.RecordFailure("shell", `{"command":"bad"}`)
	}
	if !c.IsTripped() {
		t.Error("breaker should be re-armable after Reset")
	}
}

func TestAlreadyTrippedShortCircuits(t *testing.T) {
	c := NewCircuitBreaker()
	for i := 0; i < 3; i++ {
		c.RecordFailure("shell", `{"command":"bad"}`)
	}
	// Once tripped, subsequent calls report tripped without changing state.
	if !c.RecordFailure("other", "x") {
		t.Error("RecordFailure on a tripped breaker should return true")
	}
	if !c.RecordOutput("anything") {
		t.Error("RecordOutput on a tripped breaker should return true")
	}
}
