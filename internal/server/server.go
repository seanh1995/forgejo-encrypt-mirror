package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/queue"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/webhook"
)

// Config holds the parameters needed to start the HTTP server.
type Config struct {
	// Address is the host:port to listen on, e.g. ":8080".
	Address string

	// WebhookSecret is the shared secret used to validate Forgejo webhook
	// signatures. If empty, signature verification is skipped.
	WebhookSecret string

	// Queue receives jobs extracted from valid push events. May be nil, in
	// which case webhook events are logged but not enqueued.
	Queue *queue.Queue
}

// Start builds the HTTP server and serves requests until ctx is canceled,
// at which point it performs a graceful shutdown.
func Start(ctx context.Context, cfg Config) error {

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	})

	webhookHandler := &webhook.Handler{Secret: cfg.WebhookSecret}
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