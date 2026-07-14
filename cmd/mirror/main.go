package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/audit"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/config"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
	ghclient "github.com/seanh1995/forgejo-encrypt-mirror/internal/github"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/logging"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/metrics"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/mirror"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/queue"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/server"
)

// version is the build version, set at build time via
// -ldflags "-X main.version=...". Defaults to "dev" for local builds.
var version = "dev"

func main() {
	logging.Init()

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	metrics.BuildInfo.WithLabelValues(version).Set(1)

	slog.Info("starting forgejo encrypt mirror", "version", version)

	var auditLogger *audit.Logger
	if cfg.Server.AuditLogPath != "" {
		auditLogger, err = audit.Open(cfg.Server.AuditLogPath)
		if err != nil {
			slog.Error("open audit log", "error", err)
			os.Exit(1)
		}
		defer auditLogger.Close()
	} else {
		auditLogger = audit.New(os.Stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine, err := gitengine.New(cfg.Git.CacheDir)
	if err != nil {
		slog.Error("init git engine", "error", err)
		os.Exit(1)
	}

	recipients, err := encrypt.LoadRecipients(cfg.Encryption.Recipient)
	if err != nil {
		slog.Error("load encryption recipients", "error", err)
		os.Exit(1)
	}
	slog.Info("loaded encryption recipients", "count", len(recipients))

	rotationStatePath := filepath.Join(cfg.Git.CacheDir, encrypt.RotationStateFileName)
	rotated, _, err := encrypt.DetectRotation(rotationStatePath, recipients)
	if err != nil {
		slog.Error("encryption rotation check", "error", err)
		os.Exit(1)
	}
	if rotated {
		slog.Warn("encryption recipients changed since last run; new encrypted commits will use the updated recipient set (existing history remains decryptable by whichever recipients originally encrypted it)")
		auditLogger.Log("encryption.key_rotation", "detected", nil)
		metrics.KeyRotations.Inc()
	}

	jobQueue := queue.New(100)
	jobQueue.StartCleanup(ctx, 10*time.Minute, 24*time.Hour)

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
		slog.Info("processing job", "job_id", job.ID, "owner", job.Owner, "repo", job.Repo, "branch", job.Branch, "commit", job.Commit)

		remoteURL, err := forgejoRemoteURL(cfg.Forgejo.URL, job.Owner, job.Repo)
		if err != nil {
			return err
		}

		cloneDone := metrics.StageTimer("clone_fetch")
		path, err := engine.EnsureMirror(ctx, job.Owner, job.Repo, remoteURL, auth)
		cloneDone()
		if err != nil {
			return err
		}

		commit, err := engine.ResolveCommit(ctx, path, job.Commit)
		if err != nil {
			return err
		}
		slog.Info("mirrored repository", "job_id", job.ID, "owner", job.Owner, "repo", job.Repo, "commit", commit)

		encPath, err := engine.EncryptedPath(job.Owner, job.Repo)
		if err != nil {
			return err
		}

		result, err := mirror.UpdateEncryptedHistory(ctx, engine, path, commit, encPath, recipients)
		if err != nil {
			return err
		}
		slog.Info("encrypted commits", "job_id", job.ID, "commits_processed", result.CommitsProcessed, "owner", job.Owner, "repo", job.Repo, "head_commit", result.HeadCommit)

		if ghClient == nil {
			slog.Info("github.token not configured, skipping push", "job_id", job.ID, "owner", job.Owner, "repo", job.Repo)
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
		pushDone := metrics.StageTimer("github_push")
		pushErr := engine.PushWorkingRepo(ctx, encPath, ghRemoteURL, "HEAD:refs/heads/"+job.Branch, ghAuth)
		pushDone()
		if pushErr != nil {
			metrics.GithubPushes.WithLabelValues("failure").Inc()
			return fmt.Errorf("push encrypted history to github: %w", pushErr)
		}
		metrics.GithubPushes.WithLabelValues("success").Inc()
		slog.Info("pushed encrypted history to github", "job_id", job.ID, "owner", job.Owner, "repo", job.Repo, "github_owner", ghOwner, "github_repo", ghRepo)

		return nil
	})
	pool.Start()
	defer pool.Stop()

	err = server.Start(ctx, server.Config{
		Address:        cfg.Server.Address,
		WebhookSecrets: cfg.WebhookSecrets(),
		StatusToken:    cfg.Server.StatusToken,
		Queue:          jobQueue,
		Audit:          auditLogger,
		Ready: func() error {
			return gitengine.CheckAvailable()
		},
	})
	if err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
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
