package events

import (
	"log"
	"runtime/debug"
	"sync"
)

// Domain categorizes events for filtered subscription.
type Domain string

const (
	DomainSystem   Domain = "system"
	DomainWebhook  Domain = "webhook"
	DomainApproval Domain = "approval"
	DomainLearning Domain = "learning"
	DomainMCP      Domain = "mcp"
	DomainDesktop  Domain = "desktop" // screen intelligence and companion observations
	DomainAgent    Domain = "agent"   // reserved for future agent-lifecycle events
)

// EventKind identifies a specific event within a domain.
type EventKind string

// Common event kinds across domains.
const (
	// System domain
	KindSystemStartup  EventKind = "system.startup"
	KindSystemShutdown EventKind = "system.shutdown"

	// Webhook domain
	KindWebhookRegistered   EventKind = "webhook.registered"
	KindWebhookUnregistered EventKind = "webhook.unregistered"

	// Approval domain
	KindApprovalRequested EventKind = "approval.requested"
	KindApprovalDecided   EventKind = "approval.decided"

	// Learning domain
	KindPreferenceLearned EventKind = "learning.preference_learned"

	// Desktop domain
	KindScreenIntel EventKind = "screen.intel"

	// MCP domain (kinds are emitted as raw strings by the MCP audit hook)
)

// Event is a structured domain event.
type Event struct {
	Topic    string
	Domain   Domain
	Kind     EventKind
	Data     interface{}
	Metadata map[string]string
}

// Handler receives events. Panics are caught and logged by the bus.
type Handler func(e Event)

// DomainHandler is a handler that subscribes to all events in a domain.
type DomainHandler struct {
	Handler Handler
	Domains []Domain // empty = all domains
}

// Bus is a typed pub/sub event bus with domain filtering.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]map[int]Handler // topic -> id -> handler
	domainSubs  map[Domain]map[int]Handler // domain -> id -> handler
	allSubs     map[int]Handler            // global subscribers
	nextID      int
}

// NewBus creates an event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[string]map[int]Handler),
		domainSubs:  make(map[Domain]map[int]Handler),
		allSubs:     make(map[int]Handler),
	}
}

// Subscribe adds a handler for a specific topic.
func (b *Bus) Subscribe(topic string, h Handler) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[int]Handler)
	}
	id := b.nextID
	b.nextID++
	b.subscribers[topic][id] = h
	return &Subscription{bus: b, topic: topic, id: id}
}

// SubscribeDomain adds a handler for all events in one or more domains.
func (b *Bus) SubscribeDomain(h Handler, domains ...Domain) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++

	sub := &Subscription{bus: b, id: id}
	if len(domains) == 0 {
		b.allSubs[id] = h
		sub.isGlobal = true
	} else {
		sub.domains = domains
		for _, d := range domains {
			if b.domainSubs[d] == nil {
				b.domainSubs[d] = make(map[int]Handler)
			}
			b.domainSubs[d][id] = h
		}
	}
	return sub
}

// Publish sends an event to all matching subscribers.
// Handlers are invoked synchronously with panic recovery — subscribers that
// perform I/O or long-running work should spawn their own goroutine internally.
func (b *Bus) Publish(e Event) {
	handlers := b.collectHandlers(e)
	for _, ih := range handlers {
		invokeSafe(ih.handler, e)
	}
}

// collectHandlers gathers all matching handlers under the read lock, then
// releases it before invocation so slow handlers never block the bus.
func (b *Bus) collectHandlers(e Event) []indexedHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	handlers := make([]indexedHandler, 0)

	if subs, ok := b.subscribers[e.Topic]; ok {
		for id, h := range subs {
			handlers = append(handlers, indexedHandler{id: id, handler: h})
		}
	}
	if subs, ok := b.domainSubs[e.Domain]; ok {
		for id, h := range subs {
			handlers = append(handlers, indexedHandler{id: id, handler: h})
		}
	}
	for id, h := range b.allSubs {
		handlers = append(handlers, indexedHandler{id: id, handler: h})
	}
	return handlers
}

type indexedHandler struct {
	id      int
	handler Handler
}

// PublishTyped is a convenience for publishing domain-scoped events.
func (b *Bus) PublishTyped(domain Domain, kind EventKind, data interface{}) {
	b.Publish(Event{
		Topic:  string(kind),
		Domain: domain,
		Kind:   kind,
		Data:   data,
	})
}

// invokeSafe runs a handler with panic recovery.
func invokeSafe(h Handler, e Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[events] panic in handler for topic=%s domain=%s kind=%s: %v\n%s",
				e.Topic, e.Domain, e.Kind, r, string(debug.Stack()))
		}
	}()
	h(e)
}

// Subscription is a handle for managed subscriptions.
type Subscription struct {
	bus      *Bus
	topic    string
	id       int
	domains  []Domain
	isGlobal bool
}

// Unsubscribe removes the subscription.
func (s *Subscription) Unsubscribe() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()

	if s.isGlobal {
		delete(s.bus.allSubs, s.id)
		return
	}

	if s.topic != "" {
		if subs, ok := s.bus.subscribers[s.topic]; ok {
			delete(subs, s.id)
		}
	}

	for _, d := range s.domains {
		if subs, ok := s.bus.domainSubs[d]; ok {
			delete(subs, s.id)
		}
	}
}
