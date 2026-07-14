package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pushPayload(t *testing.T) []byte {
	t.Helper()
	push := PushEvent{
		Ref:    "refs/heads/main",
		After:  "abc123",
		Before: "def456",
		Repository: Repository{
			Name:     "repo",
			FullName: "owner/repo",
			Owner:    User{Login: "owner"},
		},
	}
	body, err := json.Marshal(push)
	if err != nil {
		t.Fatalf("marshal push event: %v", err)
	}
	return body
}

func signedRequest(t *testing.T, body []byte, secret string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Forgejo-Event", "push")
	if secret != "" {
		req.Header.Set("X-Forgejo-Signature", computeHMAC(secret, body))
	}
	return req
}

func TestHandlerAcceptsCurrentSecret(t *testing.T) {
	body := pushPayload(t)
	var enqueued bool
	h := &Handler{
		Secrets: []string{"current-secret"},
		Enqueue: func(e RepoEvent) error { enqueued = true; return nil },
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(t, body, "current-secret"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !enqueued {
		t.Fatal("expected event to be enqueued")
	}
}

func TestHandlerRotationAcceptsOldAndNewSecret(t *testing.T) {
	body := pushPayload(t)
	h := &Handler{
		Secrets: []string{"old-secret", "new-secret"},
		Enqueue: func(e RepoEvent) error { return nil },
	}

	for _, secret := range []string{"old-secret", "new-secret"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, signedRequest(t, body, secret))
		if rec.Code != http.StatusOK {
			t.Fatalf("secret %q: expected 200, got %d", secret, rec.Code)
		}
	}
}

func TestHandlerRejectsUnknownSecret(t *testing.T) {
	body := pushPayload(t)
	h := &Handler{
		Secrets: []string{"current-secret"},
		Enqueue: func(e RepoEvent) error { return nil },
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(t, body, "wrong-secret"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerRejectsReplayedDelivery(t *testing.T) {
	body := pushPayload(t)
	var enqueueCount int
	h := &Handler{
		Secrets: []string{"secret"},
		Replay:  NewReplayCache(time.Hour),
		Enqueue: func(e RepoEvent) error { enqueueCount++; return nil },
	}

	req1 := signedRequest(t, body, "secret")
	req1.Header.Set("X-Forgejo-Delivery", "delivery-1")
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first delivery: expected 200, got %d", rec1.Code)
	}

	req2 := signedRequest(t, body, "secret")
	req2.Header.Set("X-Forgejo-Delivery", "delivery-1")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replayed delivery: expected 200 (ignored), got %d", rec2.Code)
	}

	if enqueueCount != 1 {
		t.Fatalf("expected exactly 1 enqueue, got %d", enqueueCount)
	}
}
