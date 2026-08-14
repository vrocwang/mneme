package events

import (
	"log"
	"runtime/debug"
	"sync"
)

// Domain categorizes events for filtered subscription.
type Domain string

const (
	DomainAgent        Domain = "agent"
	DomainMemory       Domain = "memory"
	DomainChannel      Domain = "channel"
	DomainCron         Domain = "cron"
	DomainSkill        Domain = "skill"
	DomainTool         Domain = "tool"
	DomainVoice        Domain = "voice"
	DomainWebhook      Domain = "webhook"
	DomainTriage       Domain = "triage"
	DomainTreeSummary  Domain = "tree_summarizer"
	DomainNotification Domain = "notification"
	DomainDevice       Domain = "device"
	DomainCompanion    Domain = "companion"
	DomainSystem       Domain = "system"
	DomainKeyring      Domain = "keyring"
	DomainAuth         Domain = "auth"
	DomainApproval     Domain = "approval"
	DomainLearning     Domain = "learning"
	DomainMCP          Domain = "mcp"
	DomainDesktop      Domain = "desktop"
	DomainTaskSource   Domain = "task_source"
	DomainCouncil      Domain = "council"
	DomainEmbedding    Domain = "embedding"
)

// EventKind identifies a specific event within a domain.
type EventKind string

// Common event kinds across domains.
const (
	// Agent domain
	KindAgentTurnStarted         EventKind = "agent.turn_started"
	KindAgentTurnCompleted       EventKind = "agent.turn_completed"
	KindAgentError               EventKind = "agent.error"
	KindSubagentSpawned          EventKind = "agent.subagent_spawned"
	KindSubagentCompleted        EventKind = "agent.subagent_completed"
	KindSubagentFailed           EventKind = "agent.subagent_failed"
	KindAgentOrchestrationStep   EventKind = "agent.orchestration_step"
	KindAgentAwaitingUser        EventKind = "agent.awaiting_user"
	KindAgentCompactionTriggered EventKind = "agent.compaction_triggered"
	KindAgentSessionExpired      EventKind = "agent.session_expired"

	// Memory domain
	KindMemoryStored                EventKind = "memory.stored"
	KindMemoryRecalled              EventKind = "memory.recalled"
	KindMemoryIngestionStarted      EventKind = "memory.ingestion_started"
	KindMemoryIngestionCompleted    EventKind = "memory.ingestion_completed"
	KindMemorySyncStarted           EventKind = "memory.sync_started"
	KindMemorySyncCompleted         EventKind = "memory.sync_completed"
	KindMemorySyncFailed            EventKind = "memory.sync_failed"
	KindMemoryArchiveCompleted      EventKind = "memory.archive_completed"
	KindMemoryTreeRebuilt           EventKind = "memory.tree_rebuilt"
	KindMemoryEntityExtracted       EventKind = "memory.entity_extracted"
	KindMemoryGraphUpdated          EventKind = "memory.graph_updated"
	KindMemoryDocumentCanonicalized EventKind = "memory.document_canonicalized"

	// Channel domain
	KindChannelInbound      EventKind = "channel.inbound_message"
	KindChannelConnected    EventKind = "channel.connected"
	KindChannelDisconnected EventKind = "channel.disconnected"
	KindChannelReaction     EventKind = "channel.reaction"

	// Cron domain
	KindCronTriggered EventKind = "cron.triggered"
	KindCronCompleted EventKind = "cron.completed"
	KindCronFailed    EventKind = "cron.failed"

	// Tool domain
	KindToolExecutionStarted   EventKind = "tool.execution_started"
	KindToolExecutionCompleted EventKind = "tool.execution_completed"
	KindToolExecutionFailed    EventKind = "tool.execution_failed"
	KindToolPolicyBlocked      EventKind = "tool.policy_blocked"

	// System domain
	KindSystemStartup  EventKind = "system.startup"
	KindSystemShutdown EventKind = "system.shutdown"
	KindHealthChanged  EventKind = "system.health_changed"

	// Approval domain
	KindApprovalRequested EventKind = "approval.requested"
	KindApprovalDecided   EventKind = "approval.decided"
	KindApprovalExpired   EventKind = "approval.expired"

	// Learning domain
	KindPreferenceLearned    EventKind = "learning.preference_learned"
	KindLearningCacheRebuilt EventKind = "learning.cache_rebuilt"

	// MCP domain
	KindMCPServerConnected    EventKind = "mcp.server_connected"
	KindMCPServerDisconnected EventKind = "mcp.server_disconnected"
	KindMCPToolListed         EventKind = "mcp.tools_listed"
	KindMCPToolCalled         EventKind = "mcp.tool_called"

	// Desktop domain
	KindDesktopCompanionActivated   EventKind = "desktop.companion_activated"
	KindDesktopCompanionDeactivated EventKind = "desktop.companion_deactivated"
	KindDesktopScreenCaptured       EventKind = "desktop.screen_captured"

	// Device domain
	KindDevicePaired       EventKind = "device.paired"
	KindDeviceUnpaired     EventKind = "device.unpaired"
	KindDeviceConnected    EventKind = "device.connected"
	KindDeviceDisconnected EventKind = "device.disconnected"

	// Keyring domain
	KindKeyringStore    EventKind = "keyring.store"
	KindKeyringRetrieve EventKind = "keyring.retrieve"
	KindKeyringDelete   EventKind = "keyring.delete"

	// Auth domain
	KindAuthSessionExpired EventKind = "auth.session_expired"
	KindAuthTokenRefreshed EventKind = "auth.token_refreshed"

	// Notification domain
	KindNotificationCreated   EventKind = "notification.created"
	KindNotificationDismissed EventKind = "notification.dismissed"

	// Triage domain
	KindTriageClassified EventKind = "triage.classified"
	KindTriageRouted     EventKind = "triage.routed"

	// Task source domain
	KindTaskSourceCreated EventKind = "task_source.created"
	KindTaskSourceUpdated EventKind = "task_source.updated"

	// Webhook domain
	KindWebhookDelivered EventKind = "webhook.delivered"
	KindWebhookFailed    EventKind = "webhook.failed"

	// Skill domain
	KindSkillExecuted   EventKind = "skill.executed"
	KindSkillRegistered EventKind = "skill.registered"

	// Trigger / subconscious domain
	KindTriggerEvaluated EventKind = "trigger.evaluated"
	KindTriggerEscalated EventKind = "trigger.escalated"

	// Model council domain
	KindCouncilDeliberationStarted   EventKind = "council.deliberation_started"
	KindCouncilDeliberationCompleted EventKind = "council.deliberation_completed"

	// Embedding domain
	KindEmbeddingModelUnhealthy EventKind = "embedding.model_unhealthy"
	KindEmbeddingModelRecovered EventKind = "embedding.model_recovered"

	// Channel domain — incoming message events
	KindChannelMessageReceived  EventKind = "channel.message_received"
	KindChannelMessageProcessed EventKind = "channel.message_processed"
	KindChannelReactionReceived EventKind = "channel.reaction_received"
	KindChannelReactionSent     EventKind = "channel.reaction_sent"

	// Webhook domain — tunnel lifecycle
	KindWebhookIncomingRequest EventKind = "webhook.incoming_request"
	KindWebhookRegistered      EventKind = "webhook.registered"
	KindWebhookUnregistered    EventKind = "webhook.unregistered"
	KindWebhookProcessed       EventKind = "webhook.processed"

	// Notification domain — triage
	KindNotificationTriaged EventKind = "notification.triaged"

	// System domain — config changes
	KindSystemAutonomyConfigChanged EventKind = "system.autonomy_config_changed"
	KindSystemAgentPathsChanged     EventKind = "system.agent_paths_changed"
	KindSystemHealthRestarted       EventKind = "system.health_restarted"

	// Keyring domain — error events
	KindKeyringDecryptFailed EventKind = "keyring.decrypt_failed"

	// Task source domain — sync events
	KindTaskSourceFetched        EventKind = "task_source.fetched"
	KindTaskSourceTaskIngested   EventKind = "task_source.task_ingested"
	KindTaskPlanAwaitingApproval EventKind = "task_source.plan_awaiting_approval"
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

// NewBus creates an event bus with the given channel capacity (unused, kept for API compat).
func NewBus(capacity int) *Bus {
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

// PublishWorkspace publishes a workspace-scoped event. The workspace path is
// added to the event metadata so subscribers can filter by workspace.
func (b *Bus) PublishWorkspace(domain Domain, kind EventKind, workspace string, data interface{}) {
	meta := map[string]string{"workspace": workspace}
	b.Publish(Event{
		Topic:    string(kind),
		Domain:   domain,
		Kind:     kind,
		Data:     data,
		Metadata: meta,
	})
}

// PublishAgent publishes an agent-scoped event with thread context.
func (b *Bus) PublishAgent(kind EventKind, threadID, model string, data interface{}) {
	meta := map[string]string{"thread_id": threadID}
	if model != "" {
		meta["model"] = model
	}
	b.Publish(Event{
		Topic:    string(kind),
		Domain:   DomainAgent,
		Kind:     kind,
		Data:     data,
		Metadata: meta,
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
