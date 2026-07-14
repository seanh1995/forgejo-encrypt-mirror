package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os/signal"
	"syscall"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/config"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/queue"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/server"
)

func main() {

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("starting forgejo encrypt mirror")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine, err := gitengine.New(cfg.Git.CacheDir)
	if err != nil {
		log.Fatal(err)
	}

	jobQueue := queue.New(100)

	var auth gitengine.Auth
	if cfg.Forgejo.Token != "" {
		auth.HeaderValue = "token " + cfg.Forgejo.Token
	}

	pool := queue.NewPool(jobQueue, 3, func(ctx context.Context, job queue.Job) error {
		log.Printf("processing job %s: %s/%s@%s (%s)", job.ID, job.Owner, job.Repo, job.Branch, job.Commit)

		remoteURL, err := forgejoRemoteURL(cfg.Forgejo.URL, job.Owner, job.Repo)
		if err != nil {
			return err
		}

		path, err := engine.EnsureMirror(ctx, job.Owner, job.Repo, remoteURL, auth)
		if err != nil {
			return err
		}

		commit, err := engine.ResolveCommit(ctx, path, job.Commit)
		if err != nil {
			return err
		}
		log.Printf("job %s: mirrored %s/%s at %s", job.ID, job.Owner, job.Repo, commit)

		// TODO: encryption pipeline (Phase 5) + GitHub push (Phase 7).
		return nil
	})
	pool.Start()
	defer pool.Stop()

	err = server.Start(ctx, server.Config{
		Address:       cfg.Server.Address,
		WebhookSecret: cfg.Forgejo.WebhookSecret,
		Queue:         jobQueue,
	})
	if err != nil {
		log.Fatal(err)
	}
}

// forgejoRemoteURL builds a clone URL for owner/repo on the configured
// Forgejo instance. Credentials are never embedded in the URL; auth is
// passed separately as an HTTP header (see gitengine.Auth) so tokens don't
// leak via process listings or git's own log/error output.
func forgejoRemoteURL(baseURL, owner, repo string) (string, error) {
	if err := gitengine.ValidateName("owner", owner); err != nil {
		return "", err
	}
	if err := gitengine.ValidateName("repo", repo); err != nil {
		return "", err
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid forgejo url: %w", err)
	}

	u.Path = fmt.Sprintf("/%s/%s.git", owner, repo)

	return u.String(), nil
}