package webhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// EnqueueFunc accepts a normalized repository event for processing (e.g. by
// a job queue). Implementations should be fast and non-blocking.
type EnqueueFunc func(RepoEvent) error

// Handler validates and processes incoming Forgejo webhook requests.
type Handler struct {
	// Secret is the shared webhook secret configured in Forgejo. If empty,
	// signature verification is skipped (not recommended for production).
	Secret string

	// Enqueue is called with the extracted repository event for each valid
	// push event. It must not be nil.
	Enqueue EnqueueFunc
}

// signatureHeaders lists the header names Forgejo/Gitea use for the
// HMAC signature, in order of preference.
var signatureHeaders = []string{"X-Forgejo-Signature", "X-Gitea-Signature", "X-Hub-Signature-256"}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		http.Error(w, "unable to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if h.Secret != "" {
		if !h.verify(r, body) {
			log.Println("webhook: signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	event := r.Header.Get("X-Forgejo-Event")
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	if event != "push" {
		// Not an error: acknowledge other event types (e.g. ping) so
		// Forgejo doesn't treat the delivery as failed.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	var push PushEvent
	if err := json.Unmarshal(body, &push); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	repoEvent, ok := push.ToRepoEvent()
	if !ok {
		// Not a branch push (e.g. tag push) or missing info; ignore.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	log.Printf("Repository changed: %s/%s@%s (%s)", repoEvent.Owner, repoEvent.Repo, repoEvent.Branch, repoEvent.Commit)

	if h.Enqueue != nil {
		if err := h.Enqueue(repoEvent); err != nil {
			log.Printf("webhook: failed to enqueue job for %s/%s: %v", repoEvent.Owner, repoEvent.Repo, err)
			http.Error(w, "failed to enqueue job", http.StatusServiceUnavailable)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) verify(r *http.Request, body []byte) bool {
	for _, name := range signatureHeaders {
		if sig := r.Header.Get(name); sig != "" {
			return VerifySignature(h.Secret, body, sig)
		}
	}
	return false
}