package server

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogStore persists events to a durable store (e.g. PostgreSQL server_logs table).
type LogStore interface {
	SaveLog(eventType, username, details string) error
}

// EventLogger writes structured events to both a file and stdout,
// and optionally to a persistent store (see SetStore).
type EventLogger struct {
	file  *os.File
	store LogStore // nil = no DB logging
}

// NewLogger opens (or creates) the log file and returns an EventLogger.
func NewLogger(path string) (*EventLogger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return &EventLogger{file: f}, nil
}

// SetStore attaches a persistent log store (called once after DB is ready).
func (l *EventLogger) SetStore(s LogStore) { l.store = s }

func (l *EventLogger) Close() { l.file.Close() }

// Log writes one event line: timestamp | EVENT_TYPE | user | details
func (l *EventLogger) Log(eventType, username, details string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s | %-15s | user=%-20s | %s\n", ts, eventType, username, details)

	log.Print(line)
	if l.file != nil {
		l.file.WriteString(line) //nolint:errcheck
	}
	if l.store != nil {
		l.store.SaveLog(eventType, username, details) //nolint:errcheck
	}
}
