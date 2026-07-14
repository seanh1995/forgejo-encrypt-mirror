package encrypt

import (
	"path/filepath"
	"testing"

	"filippo.io/age"
)

func TestDetectRotationFirstRunNotRotated(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".encryption-recipients")

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	rotated, previous, err := DetectRotation(statePath, []age.Recipient{identity.Recipient()})
	if err != nil {
		t.Fatalf("DetectRotation: %v", err)
	}
	if rotated {
		t.Fatal("first run should never be reported as rotated")
	}
	if previous != nil {
		t.Fatalf("expected nil previous fingerprints on first run, got %v", previous)
	}
}

func TestDetectRotationSameRecipientsNotRotated(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".encryption-recipients")

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	recipients := []age.Recipient{identity.Recipient()}

	if _, _, err := DetectRotation(statePath, recipients); err != nil {
		t.Fatalf("first DetectRotation: %v", err)
	}

	rotated, _, err := DetectRotation(statePath, recipients)
	if err != nil {
		t.Fatalf("second DetectRotation: %v", err)
	}
	if rotated {
		t.Fatal("expected no rotation when recipients are unchanged")
	}
}

func TestDetectRotationChangedRecipientsRotated(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".encryption-recipients")

	identityA, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity A: %v", err)
	}
	identityB, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity B: %v", err)
	}

	if _, _, err := DetectRotation(statePath, []age.Recipient{identityA.Recipient()}); err != nil {
		t.Fatalf("first DetectRotation: %v", err)
	}

	rotated, previous, err := DetectRotation(statePath, []age.Recipient{identityB.Recipient()})
	if err != nil {
		t.Fatalf("second DetectRotation: %v", err)
	}
	if !rotated {
		t.Fatal("expected rotation to be detected when recipients change")
	}
	if len(previous) != 1 {
		t.Fatalf("expected 1 previous fingerprint, got %d", len(previous))
	}
}
