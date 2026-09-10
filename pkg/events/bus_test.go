package events

import (
	"sync"
	"testing"
	"time"
)

func TestBus_PublishSubscribe(t *testing.T) {
	bus := NewBus()

	var received []string
	var mu sync.Mutex

	bus.Subscribe("approval.requested", func(e Event) {
		mu.Lock()
		received = append(received, e.Data.(string))
		mu.Unlock()
	})

	bus.Publish(Event{Topic: "approval.requested", Data: "hello"})
	bus.Publish(Event{Topic: "approval.requested", Data: "world"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("expected 2 messages, got %v", received)
	}
	if received[0] != "hello" || received[1] != "world" {
		t.Errorf("unexpected messages: %v", received)
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	bus := NewBus()

	count := 0
	sub := bus.Subscribe("test", func(e Event) { count++ })
	bus.Publish(Event{Topic: "test", Data: nil})
	sub.Unsubscribe()
	bus.Publish(Event{Topic: "test", Data: nil})

	time.Sleep(50 * time.Millisecond)

	if count != 1 {
		t.Errorf("expected 1 message after unsubscribe, got %d", count)
	}
}

func TestBus_DifferentTopics(t *testing.T) {
	bus := NewBus()

	var a, b int
	bus.Subscribe("topic.a", func(e Event) { a++ })
	bus.Subscribe("topic.b", func(e Event) { b++ })

	bus.Publish(Event{Topic: "topic.a", Data: nil})

	time.Sleep(50 * time.Millisecond)

	if a != 1 || b != 0 {
		t.Errorf("expected a=1, b=0; got a=%d, b=%d", a, b)
	}
}

func TestBus_DomainSubscription(t *testing.T) {
	bus := NewBus()

	var systemEvents, approvalEvents int
	bus.SubscribeDomain(func(e Event) { systemEvents++ }, DomainSystem)
	bus.SubscribeDomain(func(e Event) { approvalEvents++ }, DomainApproval)

	bus.PublishTyped(DomainSystem, KindSystemStartup, nil)
	bus.PublishTyped(DomainSystem, KindSystemShutdown, nil)
	bus.PublishTyped(DomainApproval, KindApprovalRequested, nil)

	time.Sleep(50 * time.Millisecond)

	if systemEvents != 2 {
		t.Errorf("expected 2 system events, got %d", systemEvents)
	}
	if approvalEvents != 1 {
		t.Errorf("expected 1 approval event, got %d", approvalEvents)
	}
}

func TestBus_GlobalSubscription(t *testing.T) {
	bus := NewBus()

	var total int
	bus.SubscribeDomain(func(e Event) { total++ }) // no domains = global

	bus.PublishTyped(DomainSystem, KindSystemStartup, nil)
	bus.PublishTyped(DomainApproval, KindApprovalDecided, nil)
	bus.PublishTyped(DomainLearning, KindPreferenceLearned, nil)

	time.Sleep(50 * time.Millisecond)

	if total != 3 {
		t.Errorf("expected 3 events via global subscription, got %d", total)
	}
}
