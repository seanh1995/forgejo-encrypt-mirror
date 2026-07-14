// Command restore decrypts an encrypted mirror (produced by cmd/mirror)
// back into a plaintext directory tree, using an age identity (private
// key) file. It supports restoring the current state of an encrypted
// repository, or a specific historical commit within it, for point-in-time
// recovery.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/logging"
)

func main() {
	logging.Init()

	var (
		identityPath  string
		encryptedPath string
		outPath       string
		commit        string
	)

	flag.StringVar(&identityPath, "identity", "", "path to an age identity (private key) file (required)")
	flag.StringVar(&encryptedPath, "encrypted", "", "path to the encrypted repository working tree, e.g. cache/<owner>/<repo>.enc (required)")
	flag.StringVar(&outPath, "out", "", "destination directory to restore decrypted files into (required)")
	flag.StringVar(&commit, "commit", "", "optional: restore the encrypted repository as of this commit hash, instead of its current HEAD (for point-in-time recovery)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s -identity <keyfile> -encrypted <path> -out <dir> [-commit <hash>]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Decrypts an encrypted mirror produced by cmd/mirror back into plaintext files.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if identityPath == "" || encryptedPath == "" || outPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(identityPath, encryptedPath, outPath, commit); err != nil {
		slog.Error("restore failed", "error", err)
		os.Exit(1)
	}
}

func run(identityPath, encryptedPath, outPath, commit string) error {
	identities, err := encrypt.LoadIdentities(identityPath)
	if err != nil {
		return fmt.Errorf("load identities: %w", err)
	}

	if err := os.MkdirAll(outPath, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	if commit == "" {
		slog.Info("restoring current state of encrypted repository", "encrypted", encryptedPath, "out", outPath)
		if err := encrypt.Decrypt(encryptedPath, outPath, identities); err != nil {
			return fmt.Errorf("decrypt: %w", err)
		}
		slog.Info("restore complete", "out", outPath)
		return nil
	}

	slog.Info("restoring encrypted repository at specific commit", "encrypted", encryptedPath, "commit", commit, "out", outPath)

	scratchDir, err := os.MkdirTemp("", "forgejo-restore-*")
	if err != nil {
		return fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchDir)

	// ExportTree only needs a readable git object database (--git-dir), so
	// this doesn't touch or depend on the encrypted repository's current
	// working-tree checkout.
	engine := &gitengine.Engine{}
	gitDir := filepath.Join(encryptedPath, ".git")
	if err := engine.ExportTree(context.Background(), gitDir, commit, scratchDir); err != nil {
		return fmt.Errorf("export commit %s: %w", commit, err)
	}

	if err := encrypt.Decrypt(scratchDir, outPath, identities); err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	slog.Info("restore complete", "out", outPath)
	return nil
}
