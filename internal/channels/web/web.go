// Package web provides a browser-based chat channel for Mneme.
// It serves an HTTP endpoint and optional WebSocket for real-time chat.
package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/simon/mneme/internal/channels"
)

const writeTimeout = 10 * time.Second

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

func (c *Channel) Name() string                    { return channels.ChannelWeb }
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
