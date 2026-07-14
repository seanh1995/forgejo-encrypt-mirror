package webhook

import "strings"

// User represents the Forgejo user embedded in webhook payloads.
type User struct {
	ID       int64  `json:"id"`
	Login    string `json:"login"`
	UserName string `json:"username"`
}

// Repository represents the Forgejo repository embedded in webhook payloads.
type Repository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    User   `json:"owner"`
}

// Commit represents a single commit within a push event.
type Commit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// PushEvent represents the payload Forgejo sends for a "push" webhook event.
type PushEvent struct {
	Ref        string     `json:"ref"`
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Repository Repository `json:"repository"`
	Commits    []Commit   `json:"commits"`
	HeadCommit *Commit    `json:"head_commit"`
}

// RepoEvent is the normalized repository information extracted from a push event.
type RepoEvent struct {
	Owner  string
	Repo   string
	Branch string
	Commit string
}

const refHeadsPrefix = "refs/heads/"

// ToRepoEvent extracts the owner, repository, branch and commit from a push
// event. It returns false if the event does not represent a branch push or
// is missing required information.
func (e *PushEvent) ToRepoEvent() (RepoEvent, bool) {
	if !strings.HasPrefix(e.Ref, refHeadsPrefix) {
		return RepoEvent{}, false
	}

	owner := e.Repository.Owner.Login
	if owner == "" {
		owner = e.Repository.Owner.UserName
	}

	commit := e.After
	if commit == "" && e.HeadCommit != nil {
		commit = e.HeadCommit.ID
	}

	if owner == "" || e.Repository.Name == "" || commit == "" {
		return RepoEvent{}, false
	}

	return RepoEvent{
		Owner:  owner,
		Repo:   e.Repository.Name,
		Branch: strings.TrimPrefix(e.Ref, refHeadsPrefix),
		Commit: commit,
	}, true
}
