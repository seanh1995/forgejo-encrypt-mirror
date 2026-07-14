// Package audit provides structured, append-only logging of
// security-relevant events (authentication decisions, job lifecycle,
// key/secret rotation, etc.) as JSON lines, so operators can reconstruct
// what happened for incident response and compliance purposes.
package audit

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Fields is a set of arbitrary structured key/value pairs attached to an
// audit Event.
type Fields map[string]any

// Event is a single audit log entry, serialized as one JSON line.
type Event struct {
	Time   time.Time `json:"time"`
	Action string    `json:"action"`
	Result string    `json:"result,omitempty"`
	Fields Fields    `json:"fields,omitempty"`
}

// Logger writes audit Events as newline-delimited JSON to an underlying
// writer. It is safe for concurrent use.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
}

// New creates a Logger that writes to w.
func New(w io.Writer) *Logger {
	return &Logger{w: w}
}

// Open creates a Logger that appends JSON lines to the file at path,
// creating it (and any parent directories) if necessary with permissions
// restricted to the owner (0600), since audit logs may reveal operational
// details (job identifiers, repository names, IP addresses).
func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, closer: f}, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// Close closes the underlying file, if any. Safe to call on a Logger that
// was not created with Open.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Log records a single audit event. result is typically "success",
// "failure", or "denied", but may be empty. fields carries any additional
// structured context (e.g. job ID, owner/repo, remote address).
func (l *Logger) Log(action, result string, fields Fields) {
	if l == nil {
		return
	}

	e := Event{
		Time:   time.Now().UTC(),
		Action: action,
		Result: result,
		Fields: fields,
	}

	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	l.w.Write(data)
}

// defaultLogger is used by the package-level Log function when no
// application-specific Logger has been wired in. It writes to stderr so it
// doesn't interleave with regular stdout logging by default.
var defaultLogger = New(os.Stderr)

// SetDefault replaces the package-level default Logger used by Log.
func SetDefault(l *Logger) {
	if l != nil {
		defaultLogger = l
	}
}

// Log records an audit event using the package-level default Logger.
func Log(action, result string, fields Fields) {
	defaultLogger.Log(action, result, fields)
}
