package voice

import (
	"strings"

	"github.com/simon/mneme/internal/config"
)

// STTFactory builds an STT engine from a VoiceConfig. Providers register a
// factory by name; BuildSTT looks one up and falls back to "system".
type STTFactory func(vc config.VoiceConfig) STTEngine

// TTSFactory builds a TTS engine from a VoiceConfig.
type TTSFactory func(vc config.VoiceConfig) TTSEngine

var (
	sttFactories = map[string]STTFactory{}
	ttsFactories = map[string]TTSFactory{}
)

// RegisterSTT registers an STT provider factory under a name. It replaces any
// previously registered factory of the same name (last registration wins).
func RegisterSTT(name string, f STTFactory) {
	sttFactories[strings.ToLower(name)] = f
}

// RegisterTTS registers a TTS provider factory under a name.
func RegisterTTS(name string, f TTSFactory) {
	ttsFactories[strings.ToLower(name)] = f
}

// STTProviderNames returns the names of all registered STT providers.
func STTProviderNames() []string {
	out := make([]string, 0, len(sttFactories))
	for name := range sttFactories {
		out = append(out, name)
	}
	return out
}

// TTSProviderNames returns the names of all registered TTS providers.
func TTSProviderNames() []string {
	out := make([]string, 0, len(ttsFactories))
	for name := range ttsFactories {
		out = append(out, name)
	}
	return out
}

// BuildSTT creates an STT engine based on the voice configuration, consulting
// the provider registry. Supported providers are registered in init() below.
// Defaults to "system" when unset or unknown.
func BuildSTT(cfg *config.Config) STTEngine {
	vc := cfg.Voice
	provider := vc.STTProvider
	if provider == "" {
		provider = "system"
	}
	if f, ok := sttFactories[strings.ToLower(provider)]; ok {
		return f(vc)
	}
	return NewSystemSTT()
}

// BuildTTS creates a TTS engine based on the voice configuration, consulting
// the provider registry. Defaults to "system" when unset or unknown.
func BuildTTS(cfg *config.Config) TTSEngine {
	vc := cfg.Voice
	provider := vc.TTSProvider
	if provider == "" {
		provider = "system"
	}
	if f, ok := ttsFactories[strings.ToLower(provider)]; ok {
		return f(vc)
	}
	return NewSystemTTS()
}

func init() {
	RegisterSTT("system", func(vc config.VoiceConfig) STTEngine {
		return NewSystemSTT()
	})
	RegisterSTT("whisper", func(vc config.VoiceConfig) STTEngine {
		model := vc.STTModel
		if model == "" {
			model = "base"
		}
		return NewWhisperSTT(model, "")
	})
	RegisterSTT("openai", func(vc config.VoiceConfig) STTEngine {
		model := vc.STTModel
		if model == "" {
			model = "whisper-1"
		}
		return NewOpenAIStt(vc.STTEndpoint, vc.STTAPIKey, model)
	})

	RegisterTTS("system", func(vc config.VoiceConfig) TTSEngine {
		return NewSystemTTS()
	})
	RegisterTTS("piper", func(vc config.VoiceConfig) TTSEngine {
		model := vc.TTSModel
		if model == "" {
			model = "en_US-lessac-medium"
		}
		return NewPiperTTS(model, "")
	})
	RegisterTTS("openai", func(vc config.VoiceConfig) TTSEngine {
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
		return NewOpenAITts(vc.TTSEndpoint, vc.TTSAPIKey, model, voice)
	})
}
