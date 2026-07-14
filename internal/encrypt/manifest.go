package encrypt

import (
	"encoding/json"
	"os"
)

// ManifestFileName is the name of the manifest committed alongside the
// encrypted files, describing how to reconstruct the original tree.
const ManifestFileName = "manifest.json"

// ManifestVersion identifies the manifest schema, allowing future formats
// to be recognized and migrated.
const ManifestVersion = 1

// Entry describes a single original file (or symlink) that was encrypted.
type Entry struct {
	// Path is the original file path, relative to the repository root,
	// using "/" separators.
	Path string `json:"path"`
	// EncPath is the path (relative to the manifest) of the encrypted
	// ".age" blob for this entry.
	EncPath string `json:"encPath"`
	// Mode is the original file's POSIX permission bits.
	Mode uint32 `json:"mode"`
	// Symlink is true if Path was a symbolic link rather than a regular
	// file, in which case Target holds the link target instead of
	// encrypting file content.
	Symlink bool `json:"symlink,omitempty"`
	// Target is the symlink target, set only when Symlink is true.
	Target string `json:"target,omitempty"`
}

// Manifest lists every file encrypted from a source tree, in enough detail
// to reconstruct the original tree on decryption.
type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// WriteManifest writes m as indented JSON to path.
func WriteManifest(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadManifest reads and parses a Manifest from path.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
