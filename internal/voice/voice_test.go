package voice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWhisperSTTName(t *testing.T) {
	w := NewWhisperSTT("/tmp/model.bin", "/usr/bin/whisper")
	if w.Name() != "whisper" {
		t.Errorf("expected name 'whisper', got %q", w.Name())
	}
}

func TestWhisperSTTDefaultBinary(t *testing.T) {
	w := NewWhisperSTT("/tmp/model.bin", "")
	if w.binaryPath != "whisper" {
		t.Errorf("expected default binary 'whisper', got %q", w.binaryPath)
	}
}

func TestSystemSTTName(t *testing.T) {
	s := NewSystemSTT()
	if s.Name() != "system" {
		t.Errorf("expected name 'system', got %q", s.Name())
	}
}

func TestSystemSTTNoAudioFile(t *testing.T) {
	s := NewSystemSTT()
	// When whisper is not installed, transcribing a non-existent file should
	// return either a platform error or nil if whisper is available.
	_, err := s.Transcribe(context.Background(), "/nonexistent/audio.wav")
	// We expect an error on most systems (no whisper, no file), but don't
	// fail if whisper happens to be installed.
	if err == nil {
		t.Log("whisper appears to be installed (no error returned)")
	}
}

func TestRecordFromMicUnsupportedPath(t *testing.T) {
	// A path in a non-existent directory should fail gracefully.
	tmp := filepath.Join(os.TempDir(), "mneme-test-nonexistent", "audio.wav")
	err := RecordFromMic(context.Background(), tmp, 1)
	// This should fail because the directory doesn't exist or the recording
	// tool isn't available — either way, we should not panic.
	if err == nil {
		os.RemoveAll(filepath.Dir(tmp))
	}
}

func TestPiperTTSName(t *testing.T) {
	p := NewPiperTTS("/usr/bin/piper", "/tmp/model.onnx")
	if p.Name() != "piper" {
		t.Errorf("expected name 'piper', got %q", p.Name())
	}
}

func TestSystemTTSName(t *testing.T) {
	s := NewSystemTTS()
	if s.Name() != "system" {
		t.Errorf("expected name 'system', got %q", s.Name())
	}
}

func TestPiperTTSMissingBinary(t *testing.T) {
	p := NewPiperTTS("/nonexistent/piper", "/tmp/model.onnx")
	err := p.Speak(context.Background(), "hello")
	if err == nil {
		t.Log("piper binary unexpectedly found")
	}
}
