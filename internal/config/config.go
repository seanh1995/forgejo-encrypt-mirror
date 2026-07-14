package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`

	Forgejo struct {
		URL           string `yaml:"url"`
		Token         string `yaml:"token"`
		WebhookSecret string `yaml:"webhookSecret"`
	} `yaml:"forgejo"`

	GitHub struct {
		// Owner is the default GitHub user/org repositories are pushed
		// under when the source (Forgejo) owner has no entry in OwnerMap.
		// If empty, the source owner's login is used as-is.
		Owner string `yaml:"owner"`
		// OwnerMap maps a Forgejo owner login to a specific GitHub
		// user/org, allowing different Forgejo users/organizations to be
		// mirrored to different GitHub destinations from a single
		// installation.
		OwnerMap map[string]string `yaml:"ownerMap"`
		// RepoNameFormat builds the destination GitHub repository name
		// from the source owner/repo. Supports the placeholders "{owner}"
		// and "{repo}". Defaults to "{owner}-{repo}" if empty, e.g.
		// "alice/app" -> "alice-app".
		RepoNameFormat string `yaml:"repoNameFormat"`
		Token          string `yaml:"token"`
		AutoCreate     bool   `yaml:"autoCreate"`
		// Private controls the visibility of GitHub repositories created
		// via AutoCreate. Defaults to true (private) if unset, since these
		// repositories hold encrypted backups.
		Private *bool `yaml:"private"`
	} `yaml:"github"`

	Encryption struct {
		Recipient string `yaml:"recipient"`
	} `yaml:"encryption"`

	Git struct {
		CacheDir string `yaml:"cacheDir"`
	} `yaml:"git"`
}

func Load(path string) (*Config, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.Git.CacheDir == "" {
		cfg.Git.CacheDir = "cache"
	}

	return &cfg, nil
}

// GitHubPrivate reports whether GitHub repositories created via
// GitHub.AutoCreate should be private. Defaults to true (private) when
// GitHub.Private is unset, since these repositories hold encrypted
// backups.
func (c *Config) GitHubPrivate() bool {
	if c.GitHub.Private == nil {
		return true
	}
	return *c.GitHub.Private
}

// GitHubDestination computes the GitHub owner and repository name to
// mirror sourceOwner/sourceRepo to, so a single installation can mirror
// any number of Forgejo owners/organizations and repositories.
//
// The destination owner is resolved in order: GitHub.OwnerMap[sourceOwner],
// then GitHub.Owner, then sourceOwner unchanged. The destination repository
// name is built from GitHub.RepoNameFormat (placeholders "{owner}" and
// "{repo}"), defaulting to "{repo}" (name unchanged) when unset. To mirror
// multiple owners into a single destination owner/org without name
// collisions, set repoNameFormat to "{owner}-{repo}".
func (c *Config) GitHubDestination(sourceOwner, sourceRepo string) (owner, repo string, err error) {
	if err := gitengine.ValidateName("owner", sourceOwner); err != nil {
		return "", "", err
	}
	if err := gitengine.ValidateName("repo", sourceRepo); err != nil {
		return "", "", err
	}

	owner = sourceOwner
	if mapped, ok := c.GitHub.OwnerMap[sourceOwner]; ok && mapped != "" {
		owner = mapped
	} else if c.GitHub.Owner != "" {
		owner = c.GitHub.Owner
	}

	format := c.GitHub.RepoNameFormat
	if format == "" {
		format = "{repo}"
	}
	repo = strings.NewReplacer("{owner}", sourceOwner, "{repo}", sourceRepo).Replace(format)

	if err := gitengine.ValidateName("owner", owner); err != nil {
		return "", "", fmt.Errorf("github destination owner: %w", err)
	}
	if err := gitengine.ValidateName("repo", repo); err != nil {
		return "", "", fmt.Errorf("github destination repo: %w", err)
	}

	return owner, repo, nil
}