package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature checks that header contains a valid HMAC-SHA256 signature
// of payload computed using secret. It supports both Forgejo/Gitea's native
// hex-encoded signature header and GitHub-style "sha256=<hex>" headers.
// Verification always fails if secret or header is empty.
func VerifySignature(secret string, payload []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}

	header = strings.TrimSpace(header)
	header = strings.TrimPrefix(header, "sha256=")

	expected := computeHMAC(secret, payload)

	// hmac.Equal performs a constant-time comparison to avoid leaking
	// timing information about how much of the signature matched.
	return hmac.Equal([]byte(strings.ToLower(header)), []byte(expected))
}

func computeHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
