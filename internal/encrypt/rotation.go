package encrypt

import (
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
)

// RotationStateFileName is the name of the file (stored under the git
// cache directory) that records the recipient fingerprints last used to
// encrypt, so key rotation can be detected across restarts.
const RotationStateFileName = ".encryption-recipients"

// DetectRotation compares recipients against the fingerprints last
// recorded at statePath (see RotationStateFileName), reporting whether
// the recipient set has changed since then. It always (re)writes statePath
// with the current fingerprints, so the next call reflects this run.
//
// A missing or empty state file is not treated as a rotation (there is
// nothing to rotate from on first run); every other difference -- keys
// added, removed, or replaced -- is.
//
// Note: because previously-produced ".age" files remain encrypted to the
// old recipients until they are next re-encrypted, callers should treat a
// detected rotation as a signal to re-run encryption for existing history
// (or document that old recipients remain able to decrypt already-mirrored
// commits until the source history is fully replayed).
func DetectRotation(statePath string, recipients []age.Recipient) (rotated bool, previous []string, err error) {
	current := RecipientFingerprints(recipients)

	data, readErr := os.ReadFile(statePath)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return false, nil, fmt.Errorf("encrypt: read rotation state: %w", readErr)
		}
		// First run: nothing to compare against.
	} else {
		previous = splitNonEmptyLines(string(data))
		rotated = !equalStrings(previous, current)
	}

	if err := writeRotationState(statePath, current); err != nil {
		return rotated, previous, err
	}

	return rotated, previous, nil
}

func writeRotationState(statePath string, fingerprints []string) error {
	data := []byte(strings.Join(fingerprints, "\n") + "\n")
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		return fmt.Errorf("encrypt: write rotation state: %w", err)
	}
	return nil
}

func splitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
