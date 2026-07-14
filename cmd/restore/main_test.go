package main

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/encrypt"
)

func TestRunRestoresCurrentState(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	tmpDir := t.TempDir()

	identityPath := filepath.Join(tmpDir, "identity.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity file: %v", err)
	}

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, encrypt.IgnoreFileName), nil, 0o644); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}
	want := []byte("restore me")
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), want, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	encDir := filepath.Join(tmpDir, "enc")
	if _, err := encrypt.Encrypt(srcDir, encDir, []age.Recipient{identity.Recipient()}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	outDir := filepath.Join(tmpDir, "out")
	if err := run(identityPath, encDir, outDir, ""); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "file.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("restored content = %q, want %q", got, want)
	}
}

func TestRunFailsWithMissingIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	err := run(filepath.Join(tmpDir, "does-not-exist.txt"), tmpDir, filepath.Join(tmpDir, "out"), "")
	if err == nil {
		t.Fatal("expected error for missing identity file, got nil")
	}
}
