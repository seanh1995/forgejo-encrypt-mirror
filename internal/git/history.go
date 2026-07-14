package git

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fieldSep separates fields in the machine-readable commit metadata format
// used by GetCommitMeta. 0x1F (unit separator) is vanishingly unlikely to
// appear in author/committer names, emails, or dates.
const fieldSep = "\x1f"

// SourceTrailer is the git trailer key written into encrypted-history
// commit messages to record which source-repository commit produced them.
// It lets LastSourceCommit recover mirroring progress from the encrypted
// repository alone, without separate state storage.
const SourceTrailer = "Source-Commit"

// CommitMeta describes the author/committer identity, timestamps, and
// message of a single commit, used to reproduce equivalent metadata on an
// encrypted commit derived from it.
type CommitMeta struct {
	Hash           string
	AuthorName     string
	AuthorEmail    string
	AuthorDate     string
	CommitterName  string
	CommitterEmail string
	CommitterDate  string
	Message        string
}

// ListCommits returns the commit hashes reachable from to, ordered
// oldest-first, within the repository at path. If from is non-empty, only
// commits reachable from to but not from from are returned (an incremental
// range); otherwise the full ancestry of to is returned, so the first
// mirror of a repository reproduces its entire history.
func (e *Engine) ListCommits(ctx context.Context, path, from, to string) ([]string, error) {
	rangeSpec := to
	if from != "" {
		rangeSpec = from + ".." + to
	}

	out, err := e.run(ctx, path, "rev-list", "--reverse", rangeSpec)
	if err != nil {
		return nil, fmt.Errorf("git: list commits %s: %w", rangeSpec, err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}

	return strings.Split(out, "\n"), nil
}

// GetCommitMeta reads author/committer identity, timestamps, and message
// for commit within the repository at path (bare or working, --git-dir is
// used either way).
func (e *Engine) GetCommitMeta(ctx context.Context, path, commit string) (CommitMeta, error) {
	format := strings.Join([]string{"%H", "%an", "%ae", "%ad", "%cn", "%ce", "%cd", "%B"}, fieldSep)
	out, err := e.run(ctx, path, "log", "-1", "--date=iso-strict", "--format="+format, commit)
	if err != nil {
		return CommitMeta{}, fmt.Errorf("git: read commit metadata for %s: %w", commit, err)
	}

	fields := strings.SplitN(out, fieldSep, 8)
	if len(fields) != 8 {
		return CommitMeta{}, fmt.Errorf("git: unexpected commit metadata format for %s", commit)
	}

	return CommitMeta{
		Hash:           fields[0],
		AuthorName:     fields[1],
		AuthorEmail:    fields[2],
		AuthorDate:     fields[3],
		CommitterName:  fields[4],
		CommitterEmail: fields[5],
		CommitterDate:  fields[6],
		Message:        strings.TrimRight(fields[7], "\n"),
	}, nil
}

// ExportTree extracts the full file tree of commit from the repository at
// repoPath into destDir, via `git archive`. destDir is created if needed;
// its existing contents are not cleared automatically (see
// ClearWorkingTree for that).
func (e *Engine) ExportTree(ctx context.Context, repoPath, commit, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("git: create export dir: %w", err)
	}

	args := []string{"--git-dir=" + repoPath, "archive", "--format=tar", commit}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git: archive %s: %w", commit, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git: archive %s: %w", commit, err)
	}

	extractErr := extractTar(stdout, destDir)
	// tar.Reader's Next() returns io.EOF as soon as it sees the two
	// zero-filled end-of-archive blocks, without necessarily consuming
	// every byte git archive writes (e.g. additional padding to a block
	// boundary). Drain whatever remains so the pipe never fills up and
	// blocks git archive's write, which would otherwise deadlock Wait.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if waitErr != nil {
		return fmt.Errorf("git: archive %s: %v: %s", commit, waitErr, strings.TrimSpace(stderr.String()))
	}
	if extractErr != nil {
		return fmt.Errorf("git: extract archive for %s: %w", commit, extractErr)
	}

	return nil
}

// extractTar writes the contents of the tar stream r into destDir,
// recreating directories, regular files, and symlinks. It rejects any
// entry whose name would escape destDir.
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode&0o777))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Remove any existing entry before recreating the symlink.
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			// Skip anything else (devices, fifos, etc.) - not meaningful
			// to mirror.
		}
	}
}

// safeJoin joins base and name, rejecting any name that would escape base
// via ".." segments or an absolute path.
func safeJoin(base, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." {
		return "", fmt.Errorf("git: unsafe archive entry %q", name)
	}
	return filepath.Join(base, cleaned), nil
}

// ClearWorkingTree removes every entry in dir except ".git", so a working
// repository can be repopulated from scratch before committing an updated
// (e.g. re-encrypted) snapshot.
func ClearWorkingTree(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

// InitWorkingRepo ensures a normal (non-bare) git repository exists at dir,
// creating it with `git init` if absent.
func (e *Engine) InitWorkingRepo(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("git: create working repo dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "init", dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git: init %s: %v: %s", dir, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// CommitAll stages every change in the working repository at dir and
// commits it using meta's author/committer identity and timestamps,
// appending a "Source-Commit: <hash>" trailer to the message so the
// mapping back to the source commit can be recovered later. The commit is
// created with --allow-empty so every source commit maps to exactly one
// encrypted commit (even if encryption happened to produce byte-identical
// output, e.g. an empty source commit), keeping LastSourceCommit's
// resume-point tracking exact. It returns the new commit hash.
func (e *Engine) CommitAll(ctx context.Context, dir string, meta CommitMeta) (string, error) {
	if _, err := e.runIn(ctx, dir, "add", "-A"); err != nil {
		return "", fmt.Errorf("git: stage changes: %w", err)
	}

	message := meta.Message
	if !strings.Contains(message, SourceTrailer+":") {
		message = strings.TrimRight(message, "\n") + "\n\n" + SourceTrailer + ": " + meta.Hash + "\n"
	}

	args := []string{"commit", "--allow-empty", "-m", message}
	env := []string{
		"GIT_AUTHOR_NAME=" + orDefault(meta.AuthorName, "unknown"),
		"GIT_AUTHOR_EMAIL=" + orDefault(meta.AuthorEmail, "unknown@example.com"),
		"GIT_AUTHOR_DATE=" + meta.AuthorDate,
		"GIT_COMMITTER_NAME=" + orDefault(meta.CommitterName, "unknown"),
		"GIT_COMMITTER_EMAIL=" + orDefault(meta.CommitterEmail, "unknown@example.com"),
		"GIT_COMMITTER_DATE=" + meta.CommitterDate,
	}

	if _, err := e.runInEnv(ctx, dir, env, args...); err != nil {
		return "", fmt.Errorf("git: commit: %w", err)
	}

	hash, err := e.runIn(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git: resolve new commit: %w", err)
	}

	return strings.TrimSpace(hash), nil
}

// HeadCommit returns the current HEAD commit hash of the working
// repository at dir, or "" if dir has no commits yet.
func (e *Engine) HeadCommit(ctx context.Context, dir string) (string, error) {
	out, err := e.runIn(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		if strings.Contains(err.Error(), "unknown revision or path not in the working tree") ||
			strings.Contains(err.Error(), "ambiguous argument 'HEAD'") {
			return "", nil
		}
		return "", fmt.Errorf("git: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// LastSourceCommit returns the source-repository commit hash recorded in
// the most recent commit's Source-Commit trailer within the working
// repository at dir, or "" if dir has no commits yet.
func (e *Engine) LastSourceCommit(ctx context.Context, dir string) (string, error) {
	out, err := e.runIn(ctx, dir, "log", "-1", "--format=%(trailers:key="+SourceTrailer+",valueonly)")
	if err != nil {
		// No commits yet is not an error condition here.
		if strings.Contains(err.Error(), "does not have any commits") {
			return "", nil
		}
		return "", fmt.Errorf("git: read last source commit: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// runIn runs git with a working directory (not --git-dir), for use against
// normal (non-bare) working repositories.
func (e *Engine) runIn(ctx context.Context, dir string, args ...string) (string, error) {
	return e.runInEnv(ctx, dir, nil, args...)
}

func (e *Engine) runInEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), extraEnv...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}
