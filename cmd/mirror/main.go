package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os/signal"
	"syscall"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/config"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
	ghclient "github.com/seanh1995/forgejo-encrypt-mirror/internal/github"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/mirror"
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

	recipients, err := encrypt.LoadRecipients(cfg.Encryption.Recipient)
	if err != nil {
		log.Fatalf("encryption config: %v", err)
	}
	log.Printf("loaded %d encryption recipient(s)", len(recipients))

	jobQueue := queue.New(100)

	var auth gitengine.Auth
	if cfg.Forgejo.Token != "" {
		auth.HeaderValue = "token " + cfg.Forgejo.Token
	}

	var ghClient *ghclient.Client
	var ghAuth gitengine.Auth
	if cfg.GitHub.Token != "" {
		ghClient = ghclient.NewClient(cfg.GitHub.Token)
		ghAuth.HeaderValue = ghclient.BasicAuthHeader(cfg.GitHub.Token)
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

		encPath, err := engine.EncryptedPath(job.Owner, job.Repo)
		if err != nil {
			return err
		}

		result, err := mirror.UpdateEncryptedHistory(ctx, engine, path, commit, encPath, recipients)
		if err != nil {
			return err
		}
		log.Printf("job %s: encrypted %d commit(s) for %s/%s, HEAD now %s", job.ID, result.CommitsProcessed, job.Owner, job.Repo, result.HeadCommit)

		if ghClient == nil {
			log.Printf("job %s: github.token not configured, skipping push for %s/%s", job.ID, job.Owner, job.Repo)
			return nil
		}

		ghOwner, ghRepo, err := cfg.GitHubDestination(job.Owner, job.Repo)
		if err != nil {
			return fmt.Errorf("resolve github destination: %w", err)
		}

		if cfg.GitHub.AutoCreate {
			if err := ghClient.EnsureRepository(ctx, ghOwner, ghRepo, cfg.GitHubPrivate()); err != nil {
				return fmt.Errorf("ensure github repository: %w", err)
			}
		}

		ghRemoteURL := ghclient.RepoURL(ghOwner, ghRepo)
		if err := engine.PushWorkingRepo(ctx, encPath, ghRemoteURL, "HEAD:refs/heads/"+job.Branch, ghAuth); err != nil {
			return fmt.Errorf("push encrypted history to github: %w", err)
		}
		log.Printf("job %s: pushed encrypted history for %s/%s to github %s/%s", job.ID, job.Owner, job.Repo, ghOwner, ghRepo)

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