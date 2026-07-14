package mirror

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out.String())
	}
}

func TestUpdateEncryptedHistoryRoundTrip(t *testing.T) {
	root := t.TempDir()

	sourceDir := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceDir, "init")
	if err := os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceDir, "add", "-A")
	runGit(t, sourceDir, "commit", "-m", "first commit")

	if err := os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceDir, "add", "-A")
	runGit(t, sourceDir, "commit", "-m", "second commit")

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipients := []age.Recipient{identity.Recipient()}

	engine, err := gitengine.New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}

	encPath := filepath.Join(root, "encrypted")
	ctx := context.Background()

	// engine.run passes path via --git-dir, which for a normal (non-bare)
	// repository must point at the .git directory itself.
	sourceGitDir := filepath.Join(sourceDir, ".git")

	result, err := UpdateEncryptedHistory(ctx, engine, sourceGitDir, "HEAD", encPath, recipients)
	if err != nil {
		t.Fatalf("UpdateEncryptedHistory: %v", err)
	}
	if result.CommitsProcessed != 2 {
		t.Fatalf("expected 2 commits processed, got %d", result.CommitsProcessed)
	}

	// The manifest + .age file should exist, and no plaintext file should.
	if _, err := os.Stat(filepath.Join(encPath, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected plaintext hello.txt to be absent, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(encPath, "hello.txt.age")); err != nil {
		t.Fatalf("expected encrypted hello.txt.age to exist: %v", err)
	}

	decDir := filepath.Join(root, "decrypted")
	if err := encrypt.Decrypt(encPath, decDir, []age.Identity{identity}); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(decDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello v2" {
		t.Fatalf("expected decrypted content %q, got %q", "hello v2", string(data))
	}

	// Running again with no new source commits should be a no-op.
	result2, err := UpdateEncryptedHistory(ctx, engine, sourceGitDir, "HEAD", encPath, recipients)
	if err != nil {
		t.Fatalf("UpdateEncryptedHistory (second run): %v", err)
	}
	if result2.CommitsProcessed != 0 {
		t.Fatalf("expected 0 commits processed on second run, got %d", result2.CommitsProcessed)
	}
	if result2.HeadCommit != result.HeadCommit {
		t.Fatalf("expected HEAD to stay at %s, got %s", result.HeadCommit, result2.HeadCommit)
	}
}
