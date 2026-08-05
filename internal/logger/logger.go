package logger

import (
	"io"
	"log/slog"
	"os"
)

func New(w io.Writer, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func NewDefault() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("MNEME_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	return New(os.Stderr, level)
}
