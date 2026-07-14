package git

import (
	"fmt"
	"regexp"
)

// nameRE restricts owner/repo names to characters Forgejo/GitHub allow in
// their own naming rules. This is deliberately strict: names are used to
// build filesystem paths under the cache directory and remote URLs, so we
// must reject anything that could enable path traversal (e.g. "..", "/") or
// argument/URL injection.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidateName ensures a repository owner or name is safe to use when
// building filesystem paths and URLs.
func ValidateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%s must not be %q", kind, name)
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%s %q contains invalid characters", kind, name)
	}
	return nil
}
