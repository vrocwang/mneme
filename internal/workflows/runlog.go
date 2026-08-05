package workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxLogBytes = 65536

// LogSlice is a chunk of a run log with offset info for streaming reads.
type LogSlice struct {
	RunID      string `json:"run_id"`
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	EOF        bool   `json:"eof"`
}

// ReadLogSlice reads a slice of a run log file from offset up to maxBytes.
func ReadLogSlice(logPath string, offset int64, maxBytes int) (LogSlice, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxLogBytes
	}

	f, err := os.Open(logPath)
	if err != nil {
		return LogSlice{}, fmt.Errorf("read log: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return LogSlice{}, fmt.Errorf("stat log: %w", err)
	}

	fileSize := stat.Size()
	if offset >= fileSize {
		return LogSlice{
			Offset:     offset,
			NextOffset: offset,
			EOF:        true,
		}, nil
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return LogSlice{}, fmt.Errorf("seek log: %w", err)
	}

	readSize := int64(maxBytes)
	if offset+readSize > fileSize {
		readSize = fileSize - offset
	}

	buf := make([]byte, readSize)
	n, err := f.Read(buf)
	if err != nil {
		return LogSlice{}, fmt.Errorf("read log bytes: %w", err)
	}

	nextOffset := offset + int64(n)
	eof := nextOffset >= fileSize

	return LogSlice{
		Content:    string(buf[:n]),
		Offset:     offset,
		NextOffset: nextOffset,
		EOF:        eof,
	}, nil
}

// FindLogPath finds the log file path for a run ID in the logs directory.
func FindLogPath(logsDir, runID string) (string, bool) {
	// Sanitize runID to prevent path traversal.
	if strings.Contains(runID, "/") || strings.Contains(runID, "\\") || strings.Contains(runID, "..") {
		return "", false
	}
	path := filepath.Join(logsDir, runID+".log")
	if _, err := os.Stat(path); err == nil {
		return path, true
	}
	return "", false
}
