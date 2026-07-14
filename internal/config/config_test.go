package config

import "testing"

func TestGitHubDestinationDefaults(t *testing.T) {
	var cfg Config

	owner, repo, err := cfg.GitHubDestination("alice", "app")
	if err != nil {
		t.Fatalf("GitHubDestination: %v", err)
	}
	if owner != "alice" || repo != "app" {
		t.Fatalf("expected alice/app unchanged, got %s/%s", owner, repo)
	}
}

func TestGitHubDestinationDefaultOwner(t *testing.T) {
	var cfg Config
	cfg.GitHub.Owner = "backup"

	owner, repo, err := cfg.GitHubDestination("alice", "app")
	if err != nil {
		t.Fatalf("GitHubDestination: %v", err)
	}
	if owner != "backup" || repo != "app" {
		t.Fatalf("expected backup/app, got %s/%s", owner, repo)
	}
}

func TestGitHubDestinationRepoNameFormat(t *testing.T) {
	var cfg Config
	cfg.GitHub.Owner = "backup"
	cfg.GitHub.RepoNameFormat = "{owner}-{repo}"

	cases := []struct {
		owner, repo, wantOwner, wantRepo string
	}{
		{"alice", "app", "backup", "alice-app"},
		{"bob", "game", "backup", "bob-game"},
		{"team", "docs", "backup", "team-docs"},
	}

	for _, tc := range cases {
		gotOwner, gotRepo, err := cfg.GitHubDestination(tc.owner, tc.repo)
		if err != nil {
			t.Fatalf("GitHubDestination(%s, %s): %v", tc.owner, tc.repo, err)
		}
		if gotOwner != tc.wantOwner || gotRepo != tc.wantRepo {
			t.Fatalf("GitHubDestination(%s, %s) = %s/%s, want %s/%s",
				tc.owner, tc.repo, gotOwner, gotRepo, tc.wantOwner, tc.wantRepo)
		}
	}
}

func TestGitHubDestinationOwnerMapOverridesOwner(t *testing.T) {
	var cfg Config
	cfg.GitHub.Owner = "backup"
	cfg.GitHub.OwnerMap = map[string]string{"team": "backup-org"}
	cfg.GitHub.RepoNameFormat = "{owner}-{repo}"

	owner, repo, err := cfg.GitHubDestination("team", "docs")
	if err != nil {
		t.Fatalf("GitHubDestination: %v", err)
	}
	if owner != "backup-org" || repo != "team-docs" {
		t.Fatalf("expected backup-org/team-docs, got %s/%s", owner, repo)
	}

	// An owner not present in the map still falls back to GitHub.Owner.
	owner, repo, err = cfg.GitHubDestination("alice", "app")
	if err != nil {
		t.Fatalf("GitHubDestination: %v", err)
	}
	if owner != "backup" || repo != "alice-app" {
		t.Fatalf("expected backup/alice-app, got %s/%s", owner, repo)
	}
}

func TestGitHubDestinationRejectsInvalidNames(t *testing.T) {
	var cfg Config

	if _, _, err := cfg.GitHubDestination("../etc", "app"); err == nil {
		t.Fatal("expected error for invalid source owner")
	}
	if _, _, err := cfg.GitHubDestination("alice", "../etc"); err == nil {
		t.Fatal("expected error for invalid source repo")
	}
}

func TestGitHubDestinationRejectsInvalidComputedName(t *testing.T) {
	var cfg Config
	cfg.GitHub.RepoNameFormat = "{owner}/{repo}"

	if _, _, err := cfg.GitHubDestination("alice", "app"); err == nil {
		t.Fatal("expected error for computed repo name containing '/'")
	}
}
