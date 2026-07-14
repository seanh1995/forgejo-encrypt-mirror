// Package mirror orchestrates turning a range of commits in a source git
// mirror into an equivalent, encrypted commit history in a separate
// working repository: for each source commit, its tree is exported,
// encrypted with age, and committed with the original commit's identity
// and timestamps (see internal/git's CommitMeta/CommitAll).
package mirror

import (
	"context"
	"fmt"
	"os"

	"filippo.io/age"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
)

// Result summarizes the outcome of an UpdateEncryptedHistory run.
type Result struct {
	// CommitsProcessed is the number of source commits encrypted and
	// committed during this run.
	CommitsProcessed int
	// HeadCommit is the resulting HEAD commit hash of the encrypted
	// repository after this run, or "" if no commits existed and none
	// were processed.
	HeadCommit string
}

// UpdateEncryptedHistory brings the encrypted working repository at
// encPath up to date with sourceRef (a commit-ish, typically a branch)
// resolved within the bare source mirror at sourcePath. It:
//  1. Initializes encPath as a git working repository if it doesn't
//     already exist.
//  2. Determines the last source commit already mirrored (via the
//     Source-Commit trailer on encPath's HEAD), and lists all source
//     commits from there (exclusive) up to sourceRef (inclusive). If
//     encPath has no commits yet, the full ancestry of sourceRef is used.
//  3. For each such commit, oldest first: exports its tree to a scratch
//     directory, clears encPath's working tree, encrypts the scratch tree
//     into encPath, and commits the result using the source commit's
//     author/committer identity and timestamps.
//
// It returns how many commits were processed and the resulting HEAD.
func UpdateEncryptedHistory(ctx context.Context, engine *gitengine.Engine, sourcePath, sourceRef, encPath string, recipients []age.Recipient) (Result, error) {
	if err := engine.InitWorkingRepo(ctx, encPath); err != nil {
		return Result{}, fmt.Errorf("mirror: init encrypted repo: %w", err)
	}

	targetCommit, err := engine.ResolveCommit(ctx, sourcePath, sourceRef)
	if err != nil {
		return Result{}, fmt.Errorf("mirror: resolve %s: %w", sourceRef, err)
	}

	lastMirrored, err := engine.LastSourceCommit(ctx, encPath)
	if err != nil {
		return Result{}, fmt.Errorf("mirror: read last mirrored commit: %w", err)
	}

	commits, err := engine.ListCommits(ctx, sourcePath, lastMirrored, targetCommit)
	if err != nil {
		return Result{}, fmt.Errorf("mirror: list commits: %w", err)
	}

	encHead, err := engine.HeadCommit(ctx, encPath)
	if err != nil {
		return Result{}, fmt.Errorf("mirror: read encrypted repo HEAD: %w", err)
	}

	result := Result{HeadCommit: encHead}

	if len(commits) == 0 {
		return result, nil
	}

	scratchDir, err := os.MkdirTemp("", "forgejo-encrypt-mirror-export-*")
	if err != nil {
		return result, fmt.Errorf("mirror: create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchDir)

	// The scratch dir holds decrypted source-tree contents (potentially
	// sensitive) before encryption; restrict it to the owner only. Some
	// platforms' default temp dir permissions are already restrictive, but
	// this makes the guarantee explicit rather than relying on os.MkdirTemp
	// defaults or umask.
	if err := os.Chmod(scratchDir, 0o700); err != nil {
		return result, fmt.Errorf("mirror: secure scratch dir: %w", err)
	}

	for _, commit := range commits {
		meta, err := engine.GetCommitMeta(ctx, sourcePath, commit)
		if err != nil {
			return result, fmt.Errorf("mirror: read metadata for %s: %w", commit, err)
		}

		if err := os.RemoveAll(scratchDir); err != nil {
			return result, fmt.Errorf("mirror: clear scratch dir: %w", err)
		}
		if err := engine.ExportTree(ctx, sourcePath, commit, scratchDir); err != nil {
			return result, fmt.Errorf("mirror: export tree for %s: %w", commit, err)
		}

		if err := gitengine.ClearWorkingTree(encPath); err != nil {
			return result, fmt.Errorf("mirror: clear working tree: %w", err)
		}

		if _, err := encrypt.Encrypt(scratchDir, encPath, recipients); err != nil {
			return result, fmt.Errorf("mirror: encrypt tree for %s: %w", commit, err)
		}

		newHash, err := engine.CommitAll(ctx, encPath, meta)
		if err != nil {
			return result, fmt.Errorf("mirror: commit encrypted tree for %s: %w", commit, err)
		}

		result.CommitsProcessed++
		result.HeadCommit = newHash
	}

	return result, nil
}
