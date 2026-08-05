package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/voice"
)

// CompanionLoop manages the full desktop AI companion experience.
type CompanionLoop struct {
	log *slog.Logger

	stt    voice.STTEngine
	tts    voice.TTSEngine
	screen *ScreenCapture

	provider inference.Provider
	model    string

	// Optional: vision engine for richer screen understanding.
	vision *VisionEngine

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc // cancels in-flight Activate

	// Hotkey activation
	onActivate func()
}

// CompanionConfig configures the companion loop.
type CompanionConfig struct {
	STT      voice.STTEngine
	TTS      voice.TTSEngine
	Screen   *ScreenCapture
	Provider inference.Provider
	Model    string
	Vision   *VisionEngine // optional: enables LLM-powered screen understanding
}

// NewCompanionLoop creates a desktop companion loop.
func NewCompanionLoop(log *slog.Logger, cfg CompanionConfig) *CompanionLoop {
	return &CompanionLoop{
		log:      log,
		stt:      cfg.STT,
		tts:      cfg.TTS,
		screen:   cfg.Screen,
		provider: cfg.Provider,
		model:    cfg.Model,
		vision:   cfg.Vision,
	}
}

// VoiceEngines returns the names of the active STT and TTS engines.
func (c *CompanionLoop) VoiceEngines() (stt, tts string) {
	if c.stt != nil {
		stt = c.stt.Name()
	}
	if c.tts != nil {
		tts = c.tts.Name()
	}
	if stt == "" {
		stt = "none"
	}
	if tts == "" {
		tts = "none"
	}
	return
}

// Activate triggers the companion: capture screen → transcribe voice → LLM reasoning → TTS response.
// Call Stop() to cancel an in-flight activation.
func (c *CompanionLoop) Activate(parentCtx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("companion already active")
	}
	c.running = true
	ctx, cancel := context.WithCancel(parentCtx)
	c.cancel = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.cancel = nil
		c.running = false
		c.mu.Unlock()
		cancel()
	}()

	c.log.Info("companion activated")

	// Step 1: Capture screen context (with optional vision engine).
	var screenCtx string
	if c.screen != nil {
		sc, err := c.screen.GetScreenContext(ctx)
		if err != nil {
			c.log.Warn("screen capture failed", "error", err)
		} else if c.vision != nil && sc.ScreenshotPath != "" {
			// Use the vision engine for richer screen understanding.
			// axContext is empty — we rely on the screenshot alone since
			// companion mode doesn't capture an accessibility tree.
			desc, visErr := c.vision.AnalyzeScreen(ctx, sc.ScreenshotPath, "")
			if visErr != nil {
				c.log.Warn("vision analysis failed, using basic context", "error", visErr)
				screenCtx = fmt.Sprintf("Active window: %s", sc.ActiveWindow)
			} else {
				screenCtx = fmt.Sprintf("Active window: %s\nScreen summary: %s", sc.ActiveWindow, desc.Summary)
			}
		} else {
			screenCtx = fmt.Sprintf("Active window: %s", sc.ActiveWindow)
		}
	}

	// Step 2: Record audio and transcribe
	var userText string
	if c.stt != nil {
		audioPath := fmt.Sprintf("/tmp/mneme-voice-%d.wav", time.Now().UnixNano())
		if err := voice.RecordFromMic(ctx, audioPath, 5); err != nil {
			c.log.Warn("mic recording failed", "error", err)
		} else {
			result, err := c.stt.Transcribe(ctx, audioPath)
			if err != nil {
				c.log.Warn("STT failed", "error", err)
			} else {
				userText = result.Text
			}
		}
	}

	if userText == "" {
		c.ttsSpeak(ctx, "I didn't catch that. Please try again.")
		return nil
	}

	c.log.Info("companion heard", "text", userText)

	// Step 3: LLM reasoning
	systemPrompt := "You are a helpful desktop AI companion. You see the user's screen and hear their voice. Respond concisely — your answer will be spoken aloud. Keep responses under 2 sentences."

	var contextMsg string
	if screenCtx != "" {
		contextMsg = "Screen context: " + screenCtx
	}

	req := inference.ChatRequest{
		Model: c.model,
		Messages: []inference.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: contextMsg},
			{Role: "user", Content: userText},
		},
	}

	tokens, errs := c.provider.Chat(ctx, req)

	var responseText string
	var llmErr error
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				goto done
			}
			responseText += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				llmErr = err
				c.log.Error("LLM error", "error", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
done:
	if llmErr != nil && strings.TrimSpace(responseText) == "" {
		c.ttsSpeak(ctx, "Sorry, I encountered a problem connecting to the AI service.")
		return fmt.Errorf("companion LLM error: %w", llmErr)
	}

	c.log.Info("companion response", "text", responseText)

	if strings.TrimSpace(responseText) == "" {
		c.ttsSpeak(ctx, "I didn't catch that. Please try again.")
		return nil
	}

	// Step 4: TTS response
	return c.ttsSpeak(ctx, responseText)
}

// SetActivateCallback registers a function to be called when the companion
// should be activated by an external trigger (e.g. global hotkey).
func (c *CompanionLoop) SetActivateCallback(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onActivate = fn
}

// IsRunning reports whether the companion loop is currently active.
func (c *CompanionLoop) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Stop cancels any in-flight companion activation.
func (c *CompanionLoop) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *CompanionLoop) ttsSpeak(ctx context.Context, text string) error {
	if c.tts == nil {
		return nil
	}
	if err := c.tts.Speak(ctx, text); err != nil {
		c.log.Warn("TTS failed", "error", err)
		return err
	}
	return nil
}
