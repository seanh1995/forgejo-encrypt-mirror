package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/exists":
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	exists, err := c.RepositoryExists(context.Background(), "owner", "exists")
	if err != nil {
		t.Fatalf("RepositoryExists(exists): %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}

	exists, err = c.RepositoryExists(context.Background(), "owner", "missing")
	if err != nil {
		t.Fatalf("RepositoryExists(missing): %v", err)
	}
	if exists {
		t.Fatal("expected exists=false")
	}
}

func TestEnsureRepositoryCreatesForUser(t *testing.T) {
	var created bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/newrepo":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/users/owner":
			json.NewEncoder(w).Encode(map[string]string{"type": "User"})
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var body createRepoRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Name != "newrepo" || !body.Private {
				t.Fatalf("unexpected body: %+v", body)
			}
			created = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	if err := c.EnsureRepository(context.Background(), "owner", "newrepo", true); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}
	if !created {
		t.Fatal("expected repository to be created")
	}
}

func TestEnsureRepositoryCreatesForOrg(t *testing.T) {
	var createdPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/myorg/newrepo":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/users/myorg":
			json.NewEncoder(w).Encode(map[string]string{"type": "Organization"})
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/myorg/repos":
			createdPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	if err := c.EnsureRepository(context.Background(), "myorg", "newrepo", false); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}
	if createdPath != "/orgs/myorg/repos" {
		t.Fatalf("expected org creation endpoint to be used, got %q", createdPath)
	}
}

func TestEnsureRepositorySkipsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/existing" {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	if err := c.EnsureRepository(context.Background(), "owner", "existing", true); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}
}
