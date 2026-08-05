package jsonrpc

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/simon/mneme/pkg/events"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex

	sub := s.bus.SubscribeDomain(func(e events.Event) {
		select {
		case <-done:
			return
		default:
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			mu.Lock()
			defer mu.Unlock()

			select {
			case <-done:
				return
			default:
			}

			data, err := json.Marshal(sseEvent{
				Domain:   string(e.Domain),
				Kind:     string(e.Kind),
				Topic:    e.Topic,
				Data:     e.Data,
				Metadata: e.Metadata,
			})
			if err != nil {
				return
			}

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}()
	})

	// Block until client disconnects.
	<-r.Context().Done()

	// Signal all callbacks to stop and wait for in-flight writes to finish.
	close(done)
	sub.Unsubscribe()
	wg.Wait()

	if s.log != nil {
		s.log.Debug("sse connection closed")
	} else {
		slog.Debug("sse connection closed")
	}
}

type sseEvent struct {
	Domain   string            `json:"domain"`
	Kind     string            `json:"kind"`
	Topic    string            `json:"topic"`
	Data     interface{}       `json:"data,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
