package server

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/audit"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/queue"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/webhook"
)

// Config holds the parameters needed to start the HTTP server.
type Config struct {
	// Address is the host:port to listen on, e.g. ":8080".
	Address string

	// WebhookSecrets is the ordered list of currently-valid webhook
	// secrets used to validate Forgejo webhook signatures. If empty,
	// signature verification is skipped (not recommended for production).
	// Supporting more than one secret allows rotating the configured
	// secret without downtime: add the new secret, update Forgejo, then
	// remove the old one.
	WebhookSecrets []string

	// ReplayTTL controls how long webhook delivery IDs are remembered for
	// replay detection. If zero, a default (24h) is used. Replay
	// protection is always enabled; set a very small TTL to effectively
	// disable it.
	ReplayTTL time.Duration

	// StatusToken, if set, is required (as a bearer token in the
	// Authorization header) to access the /status and /status/{id}
	// endpoints, which otherwise expose job details (repository names,
	// branches, commit hashes, error messages) to any caller. Strongly
	// recommended for any deployment reachable outside a trusted network.
	StatusToken string

	// Queue receives jobs extracted from valid push events. May be nil, in
	// which case webhook events are logged but not enqueued.
	Queue *queue.Queue

	// Audit, if set, records security-relevant events (webhook
	// verification, replay detection, status endpoint access). If nil, no
	// audit events are recorded.
	Audit *audit.Logger
}

// Start builds the HTTP server and serves requests until ctx is canceled,
// at which point it performs a graceful shutdown.
func Start(ctx context.Context, cfg Config) error {

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	})

	webhookHandler := &webhook.Handler{
		Secrets: cfg.WebhookSecrets,
		Replay:  webhook.NewReplayCache(cfg.ReplayTTL),
		Audit:   cfg.Audit,
	}
	if cfg.Queue != nil {
		q := cfg.Queue
		webhookHandler.Enqueue = func(event webhook.RepoEvent) error {
			return q.Enqueue(queue.Job{
				Owner:  event.Owner,
				Repo:   event.Repo,
				Branch: event.Branch,
				Commit: event.Commit,
			})
		}
	}
	mux.Handle("/webhook", webhookHandler)

	if cfg.Queue != nil {
		mux.Handle("GET /status", requireStatusToken(cfg.StatusToken, cfg.Audit, statusListHandler(cfg.Queue)))
		mux.Handle("GET /status/{id}", requireStatusToken(cfg.StatusToken, cfg.Audit, statusHandler(cfg.Queue)))
	}

	srv := &http.Server{
		Addr:    cfg.Address,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", cfg.Address)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Println("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// requireStatusToken wraps next with a check that the request carries a
// bearer token equal to token in its Authorization header, using a
// constant-time comparison to avoid leaking timing information about a
// partial match. If token is empty, the check is skipped and a warning is
// logged once per process so operators notice status endpoints are
// unauthenticated.
func requireStatusToken(token string, logger *audit.Logger, next http.HandlerFunc) http.Handler {
	if token == "" {
		warnStatusUnauthenticatedOnce()
		return next
	}

	expected := []byte("Bearer " + token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if len(got) != len(expected) || !hmac.Equal(got, expected) {
			if logger != nil {
				logger.Log("status.access", "denied", audit.Fields{"remote": r.RemoteAddr, "path": r.URL.Path})
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if logger != nil {
			logger.Log("status.access", "success", audit.Fields{"remote": r.RemoteAddr, "path": r.URL.Path})
		}
		next(w, r)
	})
}

var warnStatusUnauthenticatedOnce = sync.OnceFunc(func() {
	log.Println("server: WARNING: status.token is not configured; /status endpoints are unauthenticated")
})

// statusHandler returns the status record for a single job by ID.
func statusHandler(q *queue.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		rec, ok := q.Status(id)
		if !ok {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		writeJSON(w, rec)
	}
}

// statusListHandler returns status records for all known jobs.
func statusListHandler(q *queue.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, q.Statuses())
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("server: failed to encode JSON response: %v", err)
	}
}
