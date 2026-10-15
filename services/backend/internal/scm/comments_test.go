package scm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderCommentsAndDraftBranchDiscovery(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if provider == "github" && r.Header.Get("Authorization") != "Bearer saved-token" || provider == "gitlab" && r.Header.Get("PRIVATE-TOKEN") != "saved-token" {
					t.Error("credential not sent")
				}
				if strings.HasSuffix(r.URL.Path, "/comments") || strings.HasSuffix(r.URL.Path, "/notes") {
					if r.Method == "POST" {
						var input map[string]string
						_ = json.NewDecoder(r.Body).Decode(&input)
						if input["body"] != "plan summary" {
							t.Error("comment body lost")
						}
						posts++
						w.WriteHeader(201)
						return
					}
					_, _ = w.Write([]byte(`[{"id":7,"body":"/justcd approve plan digest","created_at":"2026-01-01T00:00:00Z","user":{"id":123},"author":{"id":123}}]`))
					return
				}
				if provider == "github" {
					_, _ = w.Write([]byte(`{"state":"open","draft":true,"html_url":"https://example.com/pr/1","head":{"sha":"` + strings.Repeat("a", 40) + `","ref":"feature/demo","repo":{"full_name":"org/app"}},"base":{"repo":{"full_name":"org/app"}}}`))
				} else {
					_, _ = w.Write([]byte(`{"state":"opened","draft":true,"sha":"` + strings.Repeat("a", 40) + `","source_branch":"feature/demo","source_project_id":1,"target_project_id":1}`))
				}
			}))
			defer server.Close()
			client := Client{HTTP: server.Client()}
			comments, err := client.Comments(context.Background(), provider, server.URL, "org/app", "saved-token", 1)
			if err != nil || len(comments) != 1 || comments[0].ID != 7 {
				t.Fatalf("comments: %+v %v", comments, err)
			}
			if err = client.PostComment(context.Background(), provider, server.URL, "org/app", "saved-token", 1, "plan summary"); err != nil || posts != 1 {
				t.Fatalf("post comment: %v", err)
			}
			current, err := client.Current(context.Background(), provider, server.URL, "org/app", "saved-token", 1)
			if err != nil || current.HeadBranch != "feature/demo" || current.Closed || current.Fork || !current.Draft {
				t.Fatalf("draft PR: %+v %v", current, err)
			}
		})
	}
}

func TestCommentPaginationFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(make([]Comment, 100))
	}))
	defer server.Close()
	if comments, err := (Client{HTTP: server.Client()}).Comments(context.Background(), "github", server.URL, "org/app", "token", 1); err == nil || comments != nil {
		t.Fatal("incomplete comments accepted for approval")
	}
}

func TestSummaryCommentLifecycle(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			const marker = "<!-- justcd-summary:test -->"
			var items []Comment
			posts, updates := 0, 0
			forbidden := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if forbidden {
					w.WriteHeader(403)
					return
				}
				if r.URL.Path == "/user" {
					_, _ = w.Write([]byte(`{"id":42}`))
					return
				}
				if r.Method == http.MethodGet {
					if strings.HasSuffix(r.URL.Path, "/comments") || strings.HasSuffix(r.URL.Path, "/notes") {
						_ = json.NewEncoder(w).Encode(items)
						return
					}
					if len(items) == 0 {
						w.WriteHeader(404)
						return
					}
					_ = json.NewEncoder(w).Encode(items[len(items)-1])
					return
				}
				var payload struct{ Body string }
				_ = json.NewDecoder(r.Body).Decode(&payload)
				var item Comment
				if r.Method == http.MethodPost {
					posts++
					item.ID = int64(posts + 10)
					item.User.ID = 42
					item.Author.ID = 42
					items = append(items, item)
					w.WriteHeader(201)
				} else {
					want := http.MethodPatch
					if provider == "gitlab" {
						want = http.MethodPut
					}
					if r.Method != want {
						t.Errorf("unexpected update method %s", r.Method)
					}
					updates++
				}
				items[len(items)-1].Body = payload.Body
				_ = json.NewEncoder(w).Encode(items[len(items)-1])
			}))
			defer server.Close()
			client := Client{}
			// A copied marker in a human comment must not be adopted.
			spoof := Comment{ID: 1, Body: marker + " spoofed"}
			spoof.User.ID = 99
			spoof.Author.ID = 99
			items = append(items, spoof)
			id, err := client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, 0, marker, marker+" deploying")
			if err != nil || posts != 1 || updates != 0 {
				t.Fatal(id, posts, updates, err)
			}
			// Recover after provider success but lost DB commit, without another POST.
			recovered, err := client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, 0, marker, marker+" deploying")
			if err != nil || recovered != id || posts != 1 || updates != 0 {
				t.Fatal(recovered, posts, updates, err)
			}
			_, err = client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, id, marker, marker+" healthy new commit")
			if err != nil || posts != 1 || updates != 1 {
				t.Fatal(posts, updates, err)
			}
			items = nil // Deleted summary is recreated on the next content change.
			_, err = client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, id, marker, marker+" failed")
			if err != nil || posts != 2 {
				t.Fatal(posts, err)
			}
			// Permissions errors must surface, without falling back to new comments.
			forbidden = true
			_, err = client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, id, marker, marker+" changed")
			if err == nil || !strings.Contains(err.Error(), "403") || posts != 2 {
				t.Fatal(posts, err)
			}
			forbidden = false
			items[0].User.ID = 99
			items[0].Author.ID = 99
			_, err = client.UpsertComment(context.Background(), provider, server.URL, "org/app", "token", 1, items[0].ID, marker, marker+" changed")
			if err == nil || posts != 2 || updates != 1 {
				t.Fatal("edited unowned summary", posts, updates, err)
			}
		})
	}
}
