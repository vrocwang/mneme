// Package webhooks provides inbound webhook handling and dispatch.
// Webhooks are received from external services (GitHub, Slack, custom) and
// routed to the agent for processing.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Event is a normalized webhook event delivered to the agent.
type Event struct {
	ID         string            `json:"id"`
	TunnelUUID string            `json:"tunnel_uuid"` // matches TunnelRegistration.TunnelUUID
	Source     string            `json:"source"`
	EventType  string            `json:"event_type"`
	Payload    json.RawMessage   `json:"payload"`
	Headers    map[string]string `json:"headers,omitempty"`
	ReceivedAt time.Time         `json:"received_at"`
}

// Handler processes webhook events.
type Handler func(event Event) error

// Server listens for inbound webhooks on a local HTTP port.
type Server struct {
	log     *slog.Logger
	port    int
	secret  string
	handler Handler
	server  *http.Server
}

// NewServer creates a webhook server.
func NewServer(log *slog.Logger, port int, secret string, handler Handler) *Server {
	return &Server{
		log:     log,
		port:    port,
		secret:  secret,
		handler: handler,
	}
}

// Start begins listening for webhook requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	s.log.Info("webhook server starting", "port", s.port)
	return s.server.ListenAndServe()
}

// Stop gracefully shuts down the webhook server.
func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the full body so it can be used for both signature verification
	// and JSON parsing.
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature if secret is configured.
	if s.secret != "" {
		sig := r.Header.Get("X-Webhook-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Hub-Signature-256")
		}
		if sig == "" {
			sig = r.Header.Get("X-Slack-Signature")
		}
		if !s.verifySignature(r, sig, bodyBytes) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse payload as JSON if possible; fall back to passing raw bytes.
	var payload json.RawMessage
	if json.Valid(bodyBytes) {
		payload = json.RawMessage(bodyBytes)
	} else {
		payload = json.RawMessage(fmt.Sprintf("%q", string(bodyBytes)))
	}

	headers := make(map[string]string)
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	event := Event{
		ID:         uuid.New().String(),
		TunnelUUID: r.Header.Get("X-Tunnel-UUID"),
		Source:     r.Header.Get("X-Webhook-Source"),
		EventType:  r.Header.Get("X-Webhook-Event"),
		Payload:    payload,
		Headers:    headers,
		ReceivedAt: time.Now().UTC(),
	}

	if event.Source == "" {
		event.Source = "unknown"
	}
	if event.EventType == "" {
		event.EventType = "generic"
	}

	if s.handler != nil {
		if err := s.handler(event); err != nil {
			s.log.Warn("webhook handler error", "id", event.ID, "error", err)
			http.Error(w, "handler error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) verifySignature(r *http.Request, sig string, body []byte) bool {
	if sig == "" || s.secret == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(s.secret))

	// Slack format: "v0=<hex>" where the HMAC covers "v0:<timestamp>:<body>"
	if strings.HasPrefix(sig, "v0=") {
		sig = strings.TrimPrefix(sig, "v0=")
		ts := r.Header.Get("X-Slack-Request-Timestamp")
		if ts == "" {
			return false
		}
		mac.Write([]byte("v0:" + ts + ":"))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}

	// GitHub / generic format: "sha256=<hex>" where HMAC covers the body.
	sig = strings.TrimPrefix(sig, "sha256=")

	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}
