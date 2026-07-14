// Package git manages local mirror clones of remote repositories: cloning,
// fetching updates, and pushing to other remotes. It shells out to the git
// CLI (rather than a Go git library) so it can rely on git's own protocol,
// auth, and packfile handling.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Engine manages a local cache of bare mirror repositories keyed by
// owner/repo.
type Engine struct {
	// CacheDir is the root directory under which bare mirror repositories
	// are stored, e.g. <CacheDir>/<owner>/<repo>.git
	CacheDir string
}

// New creates an Engine rooted at cacheDir. cacheDir is created if it does
// not already exist.
func New(cacheDir string) (*Engine, error) {
	if cacheDir == "" {
		return nil, errors.New("git: cache dir must not be empty")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("git: create cache dir: %w", err)
	}
	return &Engine{CacheDir: cacheDir}, nil
}

// LocalPath returns the path to the bare mirror repository for owner/repo
// within the cache directory. It validates owner and repo to prevent path
// traversal.
func (e *Engine) LocalPath(owner, repo string) (string, error) {
	if err := ValidateName("owner", owner); err != nil {
		return "", err
	}
	if err := ValidateName("repo", repo); err != nil {
		return "", err
	}
	return filepath.Join(e.CacheDir, owner, repo+".git"), nil
}

// Auth carries optional credentials for authenticating against a remote.
// Token is sent as an HTTP "Authorization" header via git config rather
// than embedded in the remote URL, so it never appears in process listings,
// URLs passed to git, or git's own error/log output.
type Auth struct {
	// HeaderValue is the full value of the HTTP Authorization header, e.g.
	// "token <token>" or "Bearer <token>". If empty, no auth header is sent.
	HeaderValue string
}

// EnsureMirror makes sure a bare mirror of remoteURL exists locally for
// owner/repo, cloning it if absent or fetching updates if already present.
// It returns the local path to the bare repository.
func (e *Engine) EnsureMirror(ctx context.Context, owner, repo, remoteURL string, auth Auth) (string, error) {
	path, err := e.LocalPath(owner, repo)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		if err := e.fetch(ctx, path, auth); err != nil {
			return "", err
		}
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("git: create repo parent dir: %w", err)
	}

	args := e.withAuth(auth, "clone", "--mirror", remoteURL, path)
	if _, err := e.run(ctx, "", args...); err != nil {
		return "", fmt.Errorf("git: clone %s/%s: %w", owner, repo, err)
	}

	return path, nil
}

// fetch updates all refs in the bare mirror at path and prunes deleted ones.
func (e *Engine) fetch(ctx context.Context, path string, auth Auth) error {
	args := e.withAuth(auth, "remote", "update", "--prune")
	if _, err := e.run(ctx, path, args...); err != nil {
		return fmt.Errorf("git: fetch updates: %w", err)
	}
	return nil
}

// withAuth prepends a -c http.extraHeader config override to args when auth
// carries a header value, so credentials are passed via git config rather
// than as part of the command's other arguments or the remote URL.
func (e *Engine) withAuth(auth Auth, args ...string) []string {
	if auth.HeaderValue == "" {
		return args
	}
	return append([]string{"-c", "http.extraHeader=Authorization: " + auth.HeaderValue}, args...)
}

// ResolveCommit resolves ref (e.g. a branch name or commit-ish) to a full
// commit SHA within the bare repository at path.
func (e *Engine) ResolveCommit(ctx context.Context, path, ref string) (string, error) {
	out, err := e.run(ctx, path, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("git: resolve %s: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// Push pushes refspec from the bare repository at path to remoteURL.
func (e *Engine) Push(ctx context.Context, path, remoteURL, refspec string, auth Auth) error {
	args := e.withAuth(auth, "push", remoteURL, refspec)
	if _, err := e.run(ctx, path, args...); err != nil {
		return fmt.Errorf("git: push %s: %w", refspec, err)
	}
	return nil
}

// run executes git with the given arguments. If dir is non-empty, it is
// passed as --git-dir so the command operates on that bare repository
// without needing a working tree or process-wide chdir.
func (e *Engine) run(ctx context.Context, dir string, args ...string) (string, error) {
	fullArgs := args
	if dir != "" {
		fullArgs = append([]string{"--git-dir=" + dir}, args...)
	}

	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	// Never allow git to block on an interactive credential/host-key prompt;
	// callers must supply credentials via the remote URL, credential
	// helper, or SSH agent configured out-of-band.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}
