package encrypt

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const IgnoreFileName = ".encryptignore"

// IgnoreList holds a set of gitignore-style glob patterns used to exclude
// files from encryption. Only a practical subset of gitignore syntax is
// supported: blank lines and "#" comments are skipped, a leading "/"
// anchors the pattern to the repository root, and "*"/"?" match within a
// single path segment via path.Match. This is intentionally simpler than
// full gitignore semantics (no "**", no negation) but covers the common
// cases needed to exclude build artifacts, secrets, and vendor directories.
type IgnoreList struct {
	patterns []pattern
}

type pattern struct {
	glob     string
	anchored bool
}

// LoadIgnoreFile reads .encryptignore from dir, if present. A missing file
// is not an error; it simply yields an empty IgnoreList.
func LoadIgnoreFile(dir string) (*IgnoreList, error) {
	data, err := os.ReadFile(filepath.Join(dir, IgnoreFileName))
	if os.IsNotExist(err) {
		return &IgnoreList{}, nil
	}
	if err != nil {
		return nil, err
	}

	il := &IgnoreList{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")

		il.patterns = append(il.patterns, pattern{glob: line, anchored: anchored})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return il, nil
}

// Match reports whether relPath (using "/" separators, relative to the
// repository root) matches any pattern in the ignore list.
func (il *IgnoreList) Match(relPath string) bool {
	if il == nil {
		return false
	}

	relPath = filepath.ToSlash(relPath)
	segments := strings.Split(relPath, "/")

	for _, p := range il.patterns {
		if p.anchored {
			if ok, _ := path.Match(p.glob, relPath); ok {
				return true
			}
			continue
		}

		// Unanchored patterns match against the full path or any suffix
		// starting at a path segment boundary, approximating gitignore's
		// "matches at any depth" behavior for simple globs.
		for i := range segments {
			candidate := strings.Join(segments[i:], "/")
			if ok, _ := path.Match(p.glob, candidate); ok {
				return true
			}
		}
	}

	return false
}
