// Package web provides a browser-based chat channel for Mneme.
// It serves an HTTP endpoint and optional WebSocket for real-time chat.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/simon/mneme/internal/channels"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
	pongTimeout  = 60 * time.Second
)

// Channel implements the channels.Channel interface for browser-based chat.
type Channel struct {
	log      *slog.Logger
	events   chan channels.Message
	wsConns  map[*websocket.Conn]struct{}
	mu       sync.Mutex
	upgrader websocket.Upgrader
}

// New creates a web channel.
func New(log *slog.Logger) *Channel {
	if log == nil {
		log = slog.Default()
	}
	return &Channel{
		log:     log,
		events:  make(chan channels.Message, 128),
		wsConns: make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (c *Channel) Name() string                    { return "web" }
func (c *Channel) Events() <-chan channels.Message { return c.events }

func (c *Channel) Start(ctx context.Context) error {
	c.log.Info("web channel started")
	return nil
}

func (c *Channel) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for conn := range c.wsConns {
		conn.Close()
	}
	c.wsConns = make(map[*websocket.Conn]struct{})
	return nil
}

// writeMsg sends to a single connection with a write deadline to prevent
// slow clients from blocking all other connections.
func (c *Channel) writeMsg(conn *websocket.Conn, data []byte) {
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Channel) Send(ctx context.Context, msg channels.Message) error {
	c.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(c.wsConns))
	for conn := range c.wsConns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()

	data, _ := json.Marshal(msg)
	for _, conn := range conns {
		c.writeMsg(conn, data)
	}
	return nil
}

// HandleWebSocket upgrades an HTTP connection to WebSocket for real-time chat.
func (c *Channel) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log.Warn("websocket upgrade failed", "error", err)
		return
	}

	// Configure read deadline and pong handler for keep-alive detection.
	conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})

	// Ping ticker for keep-alive.
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	go func() {
		for range pingTicker.C {
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	c.mu.Lock()
	c.wsConns[conn] = struct{}{}
	c.mu.Unlock()

	defer func() {
		pingTicker.Stop()
		c.mu.Lock()
		delete(c.wsConns, conn)
		c.mu.Unlock()
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Warn("websocket read error", "error", err)
			}
			return
		}

		var msg channels.Message
		if err := json.Unmarshal(message, &msg); err != nil {
			c.log.Warn("websocket bad message", "error", err)
			continue
		}

		msg.Channel = "web"
		if msg.ID == "" {
			msg.ID = fmt.Sprintf("web-%d", time.Now().UnixNano())
		}

		select {
		case c.events <- msg:
		default:
			c.log.Warn("web channel event queue full, dropping message")
		}
	}
}

// HealthCheck reports the web channel as always healthy — it has no
// external dependencies.
func (c *Channel) HealthCheck(ctx context.Context) error {
	return nil
}

// BroadcastJSON sends a JSON message to all connected WebSocket clients.
func (c *Channel) BroadcastJSON(v interface{}) error {
	c.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(c.wsConns))
	for conn := range c.wsConns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()

	data, _ := json.Marshal(v)
	for _, conn := range conns {
		c.writeMsg(conn, data)
	}
	return nil
}

// ConnectedCount returns the number of active WebSocket connections.
func (c *Channel) ConnectedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.wsConns)
}
