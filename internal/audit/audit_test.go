package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogWritesJSONLine(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)

	l.Log("webhook.verify", "success", Fields{"owner": "alice", "repo": "app"})

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected a log line to be written")
	}

	var e Event
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if e.Action != "webhook.verify" {
		t.Fatalf("action = %q, want %q", e.Action, "webhook.verify")
	}
	if e.Result != "success" {
		t.Fatalf("result = %q, want %q", e.Result, "success")
	}
	if e.Fields["owner"] != "alice" || e.Fields["repo"] != "app" {
		t.Fatalf("fields = %v, want owner=alice repo=app", e.Fields)
	}
	if e.Time.IsZero() {
		t.Fatal("expected non-zero time")
	}
}

func TestLogNilLoggerDoesNotPanic(t *testing.T) {
	var l *Logger
	l.Log("noop", "", nil)
}

func TestOpenCreatesFileAndAppends(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/audit.log"

	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	l.Log("test.event", "success", nil)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	l2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	l2.Log("test.event2", "success", nil)
	l2.Close()
}
