package events

import (
	"sync"
	"testing"
	"time"
)

func TestBus_PublishSubscribe(t *testing.T) {
	bus := NewBus(16)

	var received []string
	var mu sync.Mutex

	bus.Subscribe("agent.message", func(e Event) {
		mu.Lock()
		received = append(received, e.Data.(string))
		mu.Unlock()
	})

	bus.Publish(Event{Topic: "agent.message", Data: "hello"})
	bus.Publish(Event{Topic: "agent.message", Data: "world"})

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
	bus := NewBus(16)

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
	bus := NewBus(16)

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
	bus := NewBus(16)

	var agentEvents, toolEvents int
	bus.SubscribeDomain(func(e Event) { agentEvents++ }, DomainAgent)
	bus.SubscribeDomain(func(e Event) { toolEvents++ }, DomainTool)

	bus.PublishTyped(DomainAgent, KindAgentTurnStarted, nil)
	bus.PublishTyped(DomainAgent, KindAgentTurnCompleted, nil)
	bus.PublishTyped(DomainTool, KindToolExecutionStarted, nil)

	time.Sleep(50 * time.Millisecond)

	if agentEvents != 2 {
		t.Errorf("expected 2 agent events, got %d", agentEvents)
	}
	if toolEvents != 1 {
		t.Errorf("expected 1 tool event, got %d", toolEvents)
	}
}

func TestBus_GlobalSubscription(t *testing.T) {
	bus := NewBus(16)

	var total int
	bus.SubscribeDomain(func(e Event) { total++ }) // no domains = global

	bus.PublishTyped(DomainAgent, KindAgentTurnStarted, nil)
	bus.PublishTyped(DomainSystem, KindSystemStartup, nil)
	bus.PublishTyped(DomainMemory, KindMemoryStored, nil)

	time.Sleep(50 * time.Millisecond)

	if total != 3 {
		t.Errorf("expected 3 events via global subscription, got %d", total)
	}
}

func TestBus_WorkspaceEvent(t *testing.T) {
	bus := NewBus(16)

	var meta map[string]string
	bus.SubscribeDomain(func(e Event) {
		meta = e.Metadata
	}, DomainMemory)

	bus.PublishWorkspace(DomainMemory, KindMemoryStored, "/home/user/Mneme", "data")

	time.Sleep(50 * time.Millisecond)

	if meta == nil || meta["workspace"] != "/home/user/Mneme" {
		t.Errorf("expected workspace metadata, got %v", meta)
	}
}

func TestBus_AgentEvent(t *testing.T) {
	bus := NewBus(16)

	var gotMeta map[string]string
	bus.SubscribeDomain(func(e Event) {
		gotMeta = e.Metadata
	}, DomainAgent)

	bus.PublishAgent(KindAgentTurnStarted, "thread-123", "claude-sonnet-4-6", nil)

	time.Sleep(50 * time.Millisecond)

	if gotMeta["thread_id"] != "thread-123" {
		t.Errorf("expected thread_id, got %v", gotMeta)
	}
	if gotMeta["model"] != "claude-sonnet-4-6" {
		t.Errorf("expected model, got %v", gotMeta)
	}
}
