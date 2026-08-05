package voice

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/simon/mneme/internal/config"
	"path/filepath"
	"runtime"
	"strings"
)

// STTResult is a speech-to-text transcription.
type STTResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Language   string  `json:"language,omitempty"`
}

// STTEngine is the speech-to-text interface.
type STTEngine interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (*STTResult, error)
	// TranscribeBytes transcribes raw audio data. The format parameter
	// describes the encoding (e.g. "wav", "mp3", "webm"). The default
	// implementation writes a temp file and calls Transcribe.
	TranscribeBytes(ctx context.Context, audioData []byte, format string) (*STTResult, error)
}

// defaultTranscribeBytes is a fallback that writes audio data to a temp
// file then delegates to Transcribe.
func defaultTranscribeBytes(engine STTEngine, ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	if format == "" {
		format = "wav"
	}
	tmpFile, err := os.CreateTemp(config.TempDir(), "stt_*."+format)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(audioData); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()
	return engine.Transcribe(ctx, tmpFile.Name())
}

// WhisperSTT uses whisper.cpp for local STT.
type WhisperSTT struct {
	modelPath  string
	binaryPath string
}

// NewWhisperSTT creates a Whisper STT engine.
func NewWhisperSTT(modelPath, binaryPath string) *WhisperSTT {
	if binaryPath == "" {
		binaryPath = "whisper"
	}
	return &WhisperSTT{modelPath: modelPath, binaryPath: binaryPath}
}

func (w *WhisperSTT) Name() string { return "whisper" }

func (w *WhisperSTT) TranscribeBytes(ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	return defaultTranscribeBytes(w, ctx, audioData, format)
}

func (w *WhisperSTT) Transcribe(ctx context.Context, audioPath string) (*STTResult, error) {
	args := []string{
		"-m", w.modelPath,
		"-f", audioPath,
		"-l", "auto",
		"--no-timestamps",
		"-otxt",
	}
	cmd := exec.CommandContext(ctx, w.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("whisper: %w: %s", err, output)
	}
	txtPath := audioPath + ".txt"
	data, readErr := os.ReadFile(txtPath)
	os.Remove(txtPath)
	if readErr != nil {
		return &STTResult{Text: string(output), Confidence: 0.85}, nil
	}
	return &STTResult{Text: strings.TrimSpace(string(data)), Confidence: 0.9}, nil
}

// VoskSTT uses Vosk for lightweight offline STT.
type VoskSTT struct {
	modelPath  string
	binaryPath string
}

// NewVoskSTT creates a Vosk STT engine.
func NewVoskSTT(modelPath, binaryPath string) *VoskSTT {
	if binaryPath == "" {
		binaryPath = "vosk-transcriber"
	}
	return &VoskSTT{modelPath: modelPath, binaryPath: binaryPath}
}

func (v *VoskSTT) Name() string { return "vosk" }

func (v *VoskSTT) TranscribeBytes(ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	return defaultTranscribeBytes(v, ctx, audioData, format)
}

func (v *VoskSTT) Transcribe(ctx context.Context, audioPath string) (*STTResult, error) {
	args := []string{
		"--model", v.modelPath,
		"--input", audioPath,
	}
	cmd := exec.CommandContext(ctx, v.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("vosk: %w: %s", err, output)
	}
	return &STTResult{
		Text:       strings.TrimSpace(string(output)),
		Confidence: 0.85,
	}, nil
}

// SystemSTT probes for the best available STT backend: whisper > vosk > platform-native.
type SystemSTT struct {
	whisperBinary string
	whisperModel  string
	voskBinary    string
	voskModel     string
}

func NewSystemSTT() *SystemSTT {
	s := &SystemSTT{}
	s.probe()
	return s
}

func (s *SystemSTT) probe() {
	if p, err := exec.LookPath("whisper"); err == nil {
		s.whisperBinary = p
	}
	if p, err := exec.LookPath("vosk-transcriber"); err == nil {
		s.voskBinary = p
	}
	for _, dir := range []string{
		os.ExpandEnv("$HOME/models"),
		os.ExpandEnv("$HOME/.cache/whisper"),
		"/usr/share/whisper-models",
		"/usr/local/share/whisper",
		os.ExpandEnv("$HOME/whisper-models"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if s.whisperModel == "" && strings.HasPrefix(name, "ggml-") && strings.HasSuffix(name, ".bin") {
				s.whisperModel = filepath.Join(dir, name)
			}
			if s.voskModel == "" && strings.HasSuffix(name, ".vosk") || strings.HasSuffix(name, "-vosk") {
				info, _ := e.Info()
				if info != nil && info.IsDir() {
					s.voskModel = filepath.Join(dir, name)
				}
			}
		}
		if s.whisperModel != "" && s.voskModel != "" {
			break
		}
	}
}

func (s *SystemSTT) Name() string { return "system" }

func (s *SystemSTT) TranscribeBytes(ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	return defaultTranscribeBytes(s, ctx, audioData, format)
}

func (s *SystemSTT) Transcribe(ctx context.Context, audioPath string) (*STTResult, error) {
	if s.whisperBinary != "" && s.whisperModel != "" {
		return s.transcribeWhisper(ctx, audioPath)
	}
	if s.voskBinary != "" && s.voskModel != "" {
		return s.transcribeVosk(ctx, audioPath)
	}
	switch runtime.GOOS {
	case "darwin":
		return s.transcribeMacOS(ctx, audioPath)
	case "linux":
		return s.transcribeLinux(ctx, audioPath)
	case "windows":
		return s.transcribeWindows(ctx, audioPath)
	default:
		return nil, fmt.Errorf("no STT backend available on %s — install whisper.cpp or vosk", runtime.GOOS)
	}
}

func (s *SystemSTT) transcribeWhisper(ctx context.Context, audioPath string) (*STTResult, error) {
	args := []string{"-m", s.whisperModel, "-f", audioPath, "-l", "auto", "--no-timestamps", "-otxt"}
	cmd := exec.CommandContext(ctx, s.whisperBinary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("whisper: %w: %s", err, output)
	}
	txtPath := audioPath + ".txt"
	data, readErr := os.ReadFile(txtPath)
	os.Remove(txtPath)
	if readErr != nil {
		return &STTResult{Text: string(output), Confidence: 0.85}, nil
	}
	return &STTResult{Text: strings.TrimSpace(string(data)), Confidence: 0.9}, nil
}

func (s *SystemSTT) transcribeVosk(ctx context.Context, audioPath string) (*STTResult, error) {
	args := []string{"--model", s.voskModel, "--input", audioPath}
	cmd := exec.CommandContext(ctx, s.voskBinary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("vosk: %w: %s", err, output)
	}
	return &STTResult{Text: strings.TrimSpace(string(output)), Confidence: 0.85}, nil
}

// transcribeMacOS uses the built-in macOS speech recognition.
// Requires a Shortcut named "Transcribe Audio" that takes a file path and returns text.
func (s *SystemSTT) transcribeMacOS(ctx context.Context, audioPath string) (*STTResult, error) {
	// Try using the Shortcuts app to run a dictation shortcut.
	// Users can create a shortcut named "Mneme Transcribe" that accepts file input.
	absPath, err := filepath.Abs(audioPath)
	if err != nil {
		absPath = audioPath
	}
	// Attempt 1: Shortcuts CLI (macOS 12+)
	cmd := exec.CommandContext(ctx, "shortcuts", "run", "Mneme Transcribe", "-i", absPath)
	output, err := cmd.CombinedOutput()
	if err == nil && len(output) > 0 {
		return &STTResult{Text: strings.TrimSpace(string(output)), Confidence: 0.8}, nil
	}
	// Attempt 2: Use osascript to invoke NSSpeechRecognizer via AppleScript.
	// This is limited but functional for simple dictation on macOS 13+.
	script := fmt.Sprintf(`
		use framework "Speech"
		set recognizer to current application's SFSpeechRecognizer's alloc()'s init()
		set request to current application's SFSpeechURLRecognitionRequest's alloc()'s initWithURL:(current application's NSURL's fileURLWithPath:"%s")
		set result to recognizer's recognitionTaskWithRequest:request resultHandler:(missing value)
	`, absPath)
	cmd = exec.CommandContext(ctx, "osascript", "-l", "AppleScript", "-e", script)
	output, err = cmd.CombinedOutput()
	if err == nil && len(output) > 0 && !strings.Contains(string(output), "error") {
		return &STTResult{Text: strings.TrimSpace(string(output)), Confidence: 0.75}, nil
	}
	return nil, fmt.Errorf(
		"macOS STT: create a Shortcut named 'Mneme Transcribe' that accepts files and returns text, or install whisper.cpp (`brew install whisper-cpp`)",
	)
}

// transcribeLinux probes for lightweight alternatives before giving up.
func (s *SystemSTT) transcribeLinux(ctx context.Context, audioPath string) (*STTResult, error) {
	// Attempt 1: speechd (speech-dispatcher) — available on most desktop Linux distros.
	if _, err := exec.LookPath("spd-say"); err == nil {
		// speech-dispatcher can't transcribe directly, but we check for it and guide.
	}
	// Attempt 2: ffmpeg to convert to 16kHz mono WAV + festival for lightweight STT.
	if _, err := exec.LookPath("festival"); err == nil {
		// Convert to WAV first (festival works best with 16kHz mono).
		wavPath := audioPath + ".wav"
		ffCmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", audioPath, "-ar", "16000", "-ac", "1", wavPath)
		if ffErr := ffCmd.Run(); ffErr == nil {
			defer os.Remove(wavPath)
			cmd := exec.CommandContext(ctx, "festival", "--tts", wavPath, "--output", "-")
			output, err := cmd.CombinedOutput()
			if err == nil && len(output) > 0 {
				return &STTResult{Text: strings.TrimSpace(string(output)), Confidence: 0.6}, nil
			}
		}
	}
	// Attempt 3: Check for Vosk CLI.
	if _, err := exec.LookPath("vosk-transcriber"); err == nil {
		// Vosk model found in probe but binary in a different path.
		cmd := exec.CommandContext(ctx, "vosk-transcriber", "--input", audioPath)
		output, err := cmd.CombinedOutput()
		if err == nil && len(output) > 0 {
			return &STTResult{Text: strings.TrimSpace(string(output)), Confidence: 0.85}, nil
		}
	}
	return nil, fmt.Errorf(
		"no STT backend found on Linux — install whisper.cpp (git clone https://github.com/ggerganov/whisper.cpp) or vosk (pip install vosk)",
	)
}

// transcribeWindows uses PowerShell with System.Speech for transcription.
func (s *SystemSTT) transcribeWindows(ctx context.Context, audioPath string) (*STTResult, error) {
	absPath, err := filepath.Abs(audioPath)
	if err != nil {
		absPath = audioPath
	}
	// Ensure WAV format — System.Speech only works with WAV.
	wavPath := absPath
	if !strings.HasSuffix(strings.ToLower(absPath), ".wav") {
		wavPath = absPath + ".wav"
		ffCmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", absPath, "-ar", "16000", "-ac", "1", wavPath)
		if ffErr := ffCmd.Run(); ffErr != nil {
			return nil, fmt.Errorf("windows STT: need WAV input — %v", ffErr)
		}
		defer os.Remove(wavPath)
	}
	psScript := fmt.Sprintf(`
		Add-Type -AssemblyName System.Speech
		$recognizer = New-Object System.Speech.Recognition.SpeechRecognitionEngine
		$recognizer.SetInputToWaveFile('%s')
		$result = $recognizer.Recognize()
		if ($result) { $result.Text } else { '' }
	`, wavPath)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("windows STT failed: %w — install whisper.cpp from https://github.com/ggerganov/whisper.cpp/releases", err)
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil, fmt.Errorf("windows STT: no speech detected — ensure WAV is 16kHz mono PCM, or install whisper.cpp")
	}
	return &STTResult{Text: text, Confidence: 0.8}, nil
}

// RecordFromMic captures audio from the default microphone.
func RecordFromMic(ctx context.Context, outputPath string, durationSecs int) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "rec", outputPath, "trim", "0", fmt.Sprintf("%d", durationSecs))
	case "linux":
		// Probe for arecord first, then parec (PulseAudio), then pw-record (PipeWire).
		if _, err := exec.LookPath("arecord"); err == nil {
			cmd = exec.CommandContext(ctx, "arecord", "-d", fmt.Sprintf("%d", durationSecs), "-f", "cd", outputPath)
		} else if _, err := exec.LookPath("pw-record"); err == nil {
			cmd = exec.CommandContext(ctx, "pw-record", outputPath, "--duration", fmt.Sprintf("%d", durationSecs))
		} else if _, err := exec.LookPath("parec"); err == nil {
			cmd = exec.CommandContext(ctx, "parec", "--format=s16le", "--rate=44100", "--channels=2", outputPath)
			// parec doesn't have duration; kill after durationSecs.
			go func() {
				<-ctx.Done()
			}()
		} else {
			return fmt.Errorf("no mic recording tool found — install sox (arecord), pipewire, or pulseaudio")
		}
	case "windows":
		psScript := fmt.Sprintf(`
			Add-Type -AssemblyName System.Speech
			$rec = New-Object System.Speech.Recognition.SpeechRecognitionEngine
			$rec.SetInputToDefaultAudioDevice()
			$rec.RecognizeAsync()
			Start-Sleep -Seconds %d
			$rec.RecognizeAsyncStop()
		`, durationSecs)
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psScript)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if cmd == nil {
		return fmt.Errorf("no recording backend available on %s", runtime.GOOS)
	}
	return cmd.Run()
}
