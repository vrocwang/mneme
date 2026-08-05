package voice

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// TTSEngine is the text-to-speech interface.
type TTSEngine interface {
	Name() string
	Speak(ctx context.Context, text string) error
	SpeakToFile(ctx context.Context, text, outputPath string) error
}

// PiperTTS uses Piper for local TTS.
type PiperTTS struct {
	modelPath  string
	binaryPath string
}

// NewPiperTTS creates a Piper TTS engine.
func NewPiperTTS(modelPath, binaryPath string) *PiperTTS {
	if binaryPath == "" {
		binaryPath = "piper"
	}
	return &PiperTTS{modelPath: modelPath, binaryPath: binaryPath}
}

func (p *PiperTTS) Name() string { return "piper" }

func (p *PiperTTS) Speak(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, p.binaryPath,
		"--model", p.modelPath,
		"--output-raw", "-",
	)
	// Pipe text to stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	go func() {
		defer stdin.Close()
		stdin.Write([]byte(text))
	}()

	// Pipe audio to aplay/play
	playCmd := exec.CommandContext(ctx, platformPlayCommand())
	playCmd.Stdin, _ = cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		return err
	}
	if err := playCmd.Start(); err != nil {
		return err
	}

	go cmd.Wait()
	return playCmd.Wait()
}

func (p *PiperTTS) SpeakToFile(ctx context.Context, text, outputPath string) error {
	cmd := exec.CommandContext(ctx, p.binaryPath,
		"--model", p.modelPath,
		"--output_file", outputPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	go func() {
		defer stdin.Close()
		stdin.Write([]byte(text))
	}()
	return cmd.Run()
}

// SystemTTS uses OS-native text-to-speech.
type SystemTTS struct{}

func NewSystemTTS() *SystemTTS { return &SystemTTS{} }

func (s *SystemTTS) Name() string { return "system" }

func (s *SystemTTS) Speak(ctx context.Context, text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "say", text)
	case "linux":
		cmd = exec.CommandContext(ctx, "espeak", text)
	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('%s')`, text)
		cmd = exec.CommandContext(ctx, "powershell", "-Command", ps)
	}

	if cmd == nil {
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Run()
}

func (s *SystemTTS) SpeakToFile(ctx context.Context, text, outputPath string) error {
	return fmt.Errorf("SystemTTS does not support file output — use PiperTTS")
}

func platformPlayCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return "play"
	case "linux":
		return "aplay"
	case "windows":
		return "powershell -Command -"
	}
	return "aplay"
}
