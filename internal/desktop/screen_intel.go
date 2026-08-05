package desktop

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ScreenIntelLoop periodically captures and analyzes the screen using the
// vision engine, publishing results for downstream consumption.
type ScreenIntelLoop struct {
	log      *slog.Logger
	screen   *ScreenCapture
	vision   *VisionEngine
	interval time.Duration

	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	onResult func(ScreenCaptureResult)
}

// ScreenCaptureResult is the output of one screen intelligence cycle.
type ScreenCaptureResult struct {
	ActiveWindow   string    `json:"active_window"`
	ScreenshotPath string    `json:"screenshot_path"`
	Summary        string    `json:"summary,omitempty"`
	Error          string    `json:"error,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// ScreenIntelConfig configures the screen intelligence loop.
type ScreenIntelConfig struct {
	Screen   *ScreenCapture
	Vision   *VisionEngine
	Interval time.Duration
	OnResult func(ScreenCaptureResult) // optional callback for each capture
}

// NewScreenIntelLoop creates a screen intelligence loop.
func NewScreenIntelLoop(log *slog.Logger, cfg ScreenIntelConfig) *ScreenIntelLoop {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	return &ScreenIntelLoop{
		log:      log,
		screen:   cfg.Screen,
		vision:   cfg.Vision,
		interval: cfg.Interval,
		onResult: cfg.OnResult,
	}
}

// SetInterval updates the capture interval safely.
func (s *ScreenIntelLoop) SetInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d > 0 {
		s.interval = d
	}
}

// Start begins the periodic screen intelligence loop.
func (s *ScreenIntelLoop) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Run once immediately.
		s.captureAndAnalyze(loopCtx)

		for {
			select {
			case <-ticker.C:
				s.captureAndAnalyze(loopCtx)
			case <-loopCtx.Done():
				return
			}
		}
	}()

	s.log.Info("screen intelligence loop started", "interval", s.interval)
}

// Stop halts the screen intelligence loop.
func (s *ScreenIntelLoop) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	s.running = false
	s.log.Info("screen intelligence loop stopped")
}

func (s *ScreenIntelLoop) captureAndAnalyze(ctx context.Context) {
	result := ScreenCaptureResult{Timestamp: time.Now().UTC()}

	if s.screen != nil {
		sc, err := s.screen.GetScreenContext(ctx)
		if err != nil {
			result.Error = "capture: " + err.Error()
			s.log.Warn("screen intel capture failed", "error", err)
		} else {
			result.ActiveWindow = sc.ActiveWindow
			result.ScreenshotPath = sc.ScreenshotPath

			if s.vision != nil && sc.ScreenshotPath != "" {
				desc, err := s.vision.AnalyzeScreen(ctx, sc.ScreenshotPath, "")
				if err != nil {
					result.Error = "vision: " + err.Error()
					s.log.Warn("screen intel vision failed", "error", err)
				} else {
					result.Summary = desc.Summary
				}
			}
		}
	}

	if s.onResult != nil {
		s.onResult(result)
	}
}
