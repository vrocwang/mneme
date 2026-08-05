package voice

import (
	"strings"

	"github.com/simon/mneme/internal/config"
)

// BuildSTT creates an STT engine based on the voice configuration.
// Supported providers: "system", "whisper", "openai".
// Defaults to "system" when unset.
func BuildSTT(cfg *config.Config) STTEngine {
	vc := cfg.Voice
	provider := vc.STTProvider
	if provider == "" {
		provider = "system"
	}

	switch provider {
	case "whisper":
		model := vc.STTModel
		if model == "" {
			model = "base"
		}
		return NewWhisperSTT(model, "")
	case "openai":
		apiKey := vc.STTAPIKey
		endpoint := vc.STTEndpoint
		model := vc.STTModel
		if model == "" {
			model = "whisper-1"
		}
		return NewOpenAIStt(endpoint, apiKey, model)
	default:
		return NewSystemSTT()
	}
}

// BuildTTS creates a TTS engine based on the voice configuration.
// Supported providers: "system", "piper", "openai".
// Defaults to "system" when unset.
func BuildTTS(cfg *config.Config) TTSEngine {
	vc := cfg.Voice
	provider := vc.TTSProvider
	if provider == "" {
		provider = "system"
	}

	switch provider {
	case "piper":
		model := vc.TTSModel
		if model == "" {
			model = "en_US-lessac-medium"
		}
		return NewPiperTTS(model, "")
	case "openai":
		apiKey := vc.TTSAPIKey
		endpoint := vc.TTSEndpoint
		// Parse TTSModel as "model:voice" (e.g. "tts-1:alloy"), defaulting
		// to "tts-1" / "alloy" when the field is empty or missing a colon.
		model, voice := vc.TTSModel, "alloy"
		if idx := strings.Index(vc.TTSModel, ":"); idx >= 0 {
			model = vc.TTSModel[:idx]
			voice = vc.TTSModel[idx+1:]
		}
		if model == "" {
			model = "tts-1"
		}
		if voice == "" {
			voice = "alloy"
		}
		return NewOpenAITts(endpoint, apiKey, model, voice)
	default:
		return NewSystemTTS()
	}
}
