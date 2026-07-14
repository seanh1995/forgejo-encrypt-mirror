package encrypt

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

func TestEncryptAndDecrypt(t *testing.T) {
	// 1. Generate real temporary age keys for this test run
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("Failed to generate test age identity: %v", err)
	}
	recipient := identity.Recipient()

	// 2. Set up temporary test directories
	tmpDir, err := os.MkdirTemp("", "mirror_test_*")
	if err != nil {
		t.Fatalf("Failed to create main temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir) // Clean up everything after test finishes

	srcDir := filepath.Join(tmpDir, "src")
	encDir := filepath.Join(tmpDir, "enc")
	decDir := filepath.Join(tmpDir, "dec")

	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	// 3. Create a mock secret file to encrypt
	testFileName := "secret.txt"
	testFileContent := []byte("this is a highly classified secret")
	err = os.WriteFile(filepath.Join(srcDir, testFileName), testFileContent, 0o644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create an empty .encryptignore file so LoadIgnoreFile doesn't error out
	err = os.WriteFile(filepath.Join(srcDir, IgnoreFileName), []byte(""), 0o644)
	if err != nil {
		t.Fatalf("Failed to write empty ignore file: %v", err)
	}

	// 4. RUN ENCRYPT
	recipients := []age.Recipient{recipient}
	_, err = Encrypt(srcDir, encDir, recipients)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify the encrypted file (.age) and manifest.json exist
	encFilePath := filepath.Join(encDir, testFileName+".age")
	if _, err := os.Stat(encFilePath); os.IsNotExist(err) {
		t.Errorf("Expected encrypted file to exist at %s, but it was missing", encFilePath)
	}

	manifestPath := filepath.Join(encDir, ManifestFileName)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("Expected manifest file to exist at %s, but it was missing", manifestPath)
	}

	// 5. RUN DECRYPT
	identities := []age.Identity{identity}
	err = Decrypt(encDir, decDir, identities)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	// 6. VERIFY the decrypted file matches our starting file exactly
	decFilePath := filepath.Join(decDir, testFileName)
	decryptedContent, err := os.ReadFile(decFilePath)
	if err != nil {
		t.Fatalf("Failed to read decrypted file: %v", err)
	}

	if string(decryptedContent) != string(testFileContent) {
		t.Errorf("Content mismatch! Got %q, wanted %q", decryptedContent, testFileContent)
	}
}
