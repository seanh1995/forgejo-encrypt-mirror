package encrypt

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// gitDir is skipped during encryption/decryption walks; it is handled
// separately by the git engine and must never be encrypted itself.
const gitDir = ".git"

// Encrypt walks srcDir and encrypts every regular file it finds (except
// those under .git or matched by .encryptignore) into destDir, mirroring
// the original directory structure with an added ".age" suffix. It writes
// a manifest.json into destDir describing every entry, including symlinks
// (whose targets are recorded but not encrypted as file content) and file
// permissions, so the tree can be reconstructed with Decrypt.
//
// destDir is created if it does not already exist. Any pre-existing
// contents of destDir are not removed automatically; callers that want a
// clean encrypted tree should clear destDir first.
func Encrypt(srcDir, destDir string, recipients []age.Recipient) (*Manifest, error) {
	if len(recipients) == 0 {
		return nil, fmt.Errorf("encrypt: no recipients provided")
	}

	ignore, err := LoadIgnoreFile(srcDir)
	if err != nil {
		return nil, fmt.Errorf("encrypt: load %s: %w", IgnoreFileName, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("encrypt: create dest dir: %w", err)
	}

	manifest := &Manifest{Version: ManifestVersion}

	walkErr := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, relErr := filepath.Rel(srcDir, p)
		if relErr != nil {
			return relErr
		}
		if relPath == "." {
			return nil
		}

		relSlash := filepath.ToSlash(relPath)

		if d.IsDir() {
			if d.Name() == gitDir {
				return filepath.SkipDir
			}
			if ignore.Match(relSlash) {
				return filepath.SkipDir
			}
			return nil
		}

		if ignore.Match(relSlash) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("encrypt: readlink %s: %w", relSlash, err)
			}
			manifest.Entries = append(manifest.Entries, Entry{
				Path:    relSlash,
				Mode:    uint32(info.Mode().Perm()),
				Symlink: true,
				Target:  target,
			})
			return nil
		}

		if !info.Mode().IsRegular() {
			// Skip devices, sockets, etc. - not meaningful to mirror.
			return nil
		}

		encRelPath := relSlash + ".age"
		encPath := filepath.Join(destDir, filepath.FromSlash(encRelPath))

		if err := os.MkdirAll(filepath.Dir(encPath), 0o755); err != nil {
			return fmt.Errorf("encrypt: create dir for %s: %w", relSlash, err)
		}

		if err := encryptFile(p, encPath, recipients); err != nil {
			return fmt.Errorf("encrypt: %s: %w", relSlash, err)
		}

		manifest.Entries = append(manifest.Entries, Entry{
			Path:    relSlash,
			EncPath: encRelPath,
			Mode:    uint32(info.Mode().Perm()),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if err := WriteManifest(filepath.Join(destDir, ManifestFileName), manifest); err != nil {
		return nil, fmt.Errorf("encrypt: write manifest: %w", err)
	}

	return manifest, nil
}

func encryptFile(srcPath, destPath string, recipients []age.Recipient) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	w, err := age.Encrypt(out, recipients...)
	if err != nil {
		return fmt.Errorf("age encrypt: %w", err)
	}

	if _, err := io.Copy(w, in); err != nil {
		return err
	}

	return w.Close()
}

// Decrypt reads the manifest at manifestDir/manifest.json and reconstructs
// the original tree under destDir, decrypting each entry's ".age" file
// (found relative to manifestDir) using identities.
func Decrypt(manifestDir, destDir string, identities []age.Identity) error {
	manifest, err := ReadManifest(filepath.Join(manifestDir, ManifestFileName))
	if err != nil {
		return fmt.Errorf("decrypt: read manifest: %w", err)
	}

	for _, entry := range manifest.Entries {
		destPath := filepath.Join(destDir, filepath.FromSlash(entry.Path))

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("decrypt: create dir for %s: %w", entry.Path, err)
		}

		if entry.Symlink {
			if err := os.Symlink(entry.Target, destPath); err != nil {
				return fmt.Errorf("decrypt: symlink %s: %w", entry.Path, err)
			}
			continue
		}

		encPath := filepath.Join(manifestDir, filepath.FromSlash(entry.EncPath))
		if err := decryptFile(encPath, destPath, identities, os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("decrypt: %s: %w", entry.Path, err)
		}
	}

	return nil
}

func decryptFile(srcPath, destPath string, identities []age.Identity, mode os.FileMode) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	r, err := age.Decrypt(in, identities...)
	if err != nil {
		return fmt.Errorf("age decrypt: %w", err)
	}

	if mode == 0 {
		mode = 0o644
	}

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return err
	}

	return nil
}
