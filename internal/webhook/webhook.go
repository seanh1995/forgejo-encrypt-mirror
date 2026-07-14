package webhook

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/audit"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/metrics"
)

// EnqueueFunc accepts a normalized repository event for processing (e.g. by
// a job queue). Implementations should be fast and non-blocking.
type EnqueueFunc func(RepoEvent) error

// Handler validates and processes incoming Forgejo webhook requests.
type Handler struct {
	// Secret is the shared webhook secret configured in Forgejo. If empty
	// and Secrets is also empty, signature verification is skipped (not
	// recommended for production). Deprecated: set Secrets instead, which
	// supports rotating to a new secret without downtime.
	Secret string

	// Secrets is an ordered list of currently-valid webhook secrets. A
	// request's signature is accepted if it matches any entry, so an
	// operator can rotate secrets by adding the new secret alongside the
	// old one, updating Forgejo, then removing the old secret once
	// deliveries using it have stopped.
	Secrets []string

	// Enqueue is called with the extracted repository event for each valid
	// push event. It must not be nil.
	Enqueue EnqueueFunc

	// Replay, if set, rejects webhook deliveries whose delivery ID has
	// already been seen, protecting against replay of a captured request.
	// If nil, replay protection is disabled.
	Replay *ReplayCache

	// Audit, if set, records security-relevant webhook decisions
	// (signature verification, replay detection). If nil, no audit
	// events are recorded for this handler.
	Audit *audit.Logger
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
		metrics.WebhookRequests.WithLabelValues("error").Inc()
		http.Error(w, "unable to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	remote := r.RemoteAddr

	secrets := h.secrets()
	if len(secrets) > 0 {
		if !h.verify(r, body, secrets) {
			slog.Warn("webhook signature verification failed", "remote", remote)
			h.audit("webhook.verify", "failure", audit.Fields{"remote": remote})
			metrics.WebhookRequests.WithLabelValues("invalid_signature").Inc()
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		h.audit("webhook.verify", "success", audit.Fields{"remote": remote})
	}

	if id := deliveryID(r.Header.Get); h.Replay != nil && h.Replay.CheckAndRemember(id) {
		slog.Warn("duplicate webhook delivery ignored", "delivery_id", id)
		h.audit("webhook.replay", "denied", audit.Fields{"remote": remote, "deliveryId": id})
		metrics.WebhookRequests.WithLabelValues("replay").Inc()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("duplicate ignored"))
		return
	}

	event := r.Header.Get("X-Forgejo-Event")
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	if event != "push" {
		// Not an error: acknowledge other event types (e.g. ping) so
		// Forgejo doesn't treat the delivery as failed.
		metrics.WebhookRequests.WithLabelValues("ignored").Inc()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	var push PushEvent
	if err := json.Unmarshal(body, &push); err != nil {
		metrics.WebhookRequests.WithLabelValues("error").Inc()
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	repoEvent, ok := push.ToRepoEvent()
	if !ok {
		// Not a branch push (e.g. tag push) or missing info; ignore.
		metrics.WebhookRequests.WithLabelValues("ignored").Inc()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	slog.Info("repository changed", "owner", repoEvent.Owner, "repo", repoEvent.Repo, "branch", repoEvent.Branch, "commit", repoEvent.Commit)

	if h.Enqueue != nil {
		if err := h.Enqueue(repoEvent); err != nil {
			slog.Error("failed to enqueue job", "owner", repoEvent.Owner, "repo", repoEvent.Repo, "error", err)
			h.audit("webhook.enqueue", "failure", audit.Fields{"owner": repoEvent.Owner, "repo": repoEvent.Repo})
			metrics.WebhookRequests.WithLabelValues("error").Inc()
			http.Error(w, "failed to enqueue job", http.StatusServiceUnavailable)
			return
		}
	}

	h.audit("webhook.enqueue", "success", audit.Fields{"owner": repoEvent.Owner, "repo": repoEvent.Repo, "branch": repoEvent.Branch, "commit": repoEvent.Commit})
	metrics.WebhookRequests.WithLabelValues("accepted").Inc()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// secrets returns every currently-valid webhook secret, combining the
// deprecated single Secret field with Secrets.
func (h *Handler) secrets() []string {
	var out []string
	if h.Secret != "" {
		out = append(out, h.Secret)
	}
	for _, s := range h.Secrets {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *Handler) verify(r *http.Request, body []byte, secrets []string) bool {
	for _, name := range signatureHeaders {
		if sig := r.Header.Get(name); sig != "" {
			for _, secret := range secrets {
				if VerifySignature(secret, body, sig) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (h *Handler) audit(action, result string, fields audit.Fields) {
	if h.Audit != nil {
		h.Audit.Log(action, result, fields)
	}
}
