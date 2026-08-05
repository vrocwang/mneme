package heartbeat

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Task is evaluated on each heartbeat.
type Task func(ctx context.Context)

// Heartbeat provides periodic background pulse.
type Heartbeat struct {
	mu       sync.Mutex
	tasks    []Task
	log      *slog.Logger
	interval time.Duration
	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(log *slog.Logger, interval time.Duration) *Heartbeat {
	if interval == 0 {
		interval = 30 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Heartbeat{log: log, interval: interval, stop: make(chan struct{}), ctx: ctx, cancel: cancel}
}

func (h *Heartbeat) Stop() {
	h.stopOnce.Do(func() {
		h.cancel()
		close(h.stop)
		h.wg.Wait()
		h.log.Info("heartbeat stopped")
	})
}

// Register adds a task to be evaluated on each tick.
func (h *Heartbeat) Register(task Task) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tasks = append(h.tasks, task)
}

// Start begins the heartbeat loop. Safe to call once; subsequent calls no-op.
func (h *Heartbeat) Start() {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	h.mu.Unlock()
	h.wg.Add(1)
	go h.loop()
	h.log.Info("heartbeat started", "interval", h.interval)
}

func (h *Heartbeat) loop() {
	defer h.wg.Done()
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			tasks := make([]Task, len(h.tasks))
			copy(tasks, h.tasks)
			h.mu.Unlock()

			for _, task := range tasks {
				task(h.ctx)
			}
		case <-h.stop:
			return
		}
	}
}
