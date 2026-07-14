// Package encrypt provides age-based encryption of a repository's working
// tree into a manifest-described set of ".age" files.
package encrypt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
)

// LoadRecipients parses one or more age X25519 recipient public keys from
// value. value may be:
//   - a single recipient string (e.g. "age1...")
//   - multiple recipients separated by newlines
//   - a path to a file on disk containing one recipient per line
//
// Blank lines and lines starting with "#" are ignored.
func LoadRecipients(value string) ([]age.Recipient, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("encrypt: no recipient configured")
	}

	if data, err := os.ReadFile(value); err == nil {
		return parseRecipients(string(data))
	}

	return parseRecipients(value)
}

func parseRecipients(data string) ([]age.Recipient, error) {
	var recipients []age.Recipient

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		r, err := age.ParseX25519Recipient(line)
		if err != nil {
			return nil, fmt.Errorf("encrypt: invalid recipient %q: %w", line, err)
		}
		recipients = append(recipients, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("encrypt: no recipients found")
	}

	return recipients, nil
}

// LoadIdentities reads age identities (private keys) from a file at path,
// for future use decrypting/restoring mirrored repositories.
func LoadIdentities(path string) ([]age.Identity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	identities, err := age.ParseIdentities(f)
	if err != nil {
		return nil, fmt.Errorf("encrypt: parse identities: %w", err)
	}
	return identities, nil
}
