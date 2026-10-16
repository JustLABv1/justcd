//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationApplicationLabelsViewsAndFavorites(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('owner','owner@example.invalid'),('viewer','viewer@example.invalid'),('other','other@example.invalid'); INSERT INTO workspaces(id,name) VALUES('w','Workspace'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('w','owner','owner'),('w','viewer','viewer'); INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('source','w','Git','https://example.invalid'); INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('cluster','w','Cluster','https://example.invalid'); INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('app','w','App','source','main','deploy','yaml','cluster');`); err != nil {
		t.Fatal(err)
	}
	server, err := New(db, config.Config{EncryptionKey: []byte(strings.Repeat("k", 32)), SessionLifetime: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"owner", "viewer", "other"} {
		if err := db.CreateSession(ctx, security.HashToken(user), security.HashToken("csrf"), user, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, user, body string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: user})
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf"})
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("got %d want %d: %s", w.Code, code, w.Body)
		}
	}
	expect(request("PUT", "/api/v1/applications/app/labels", "owner", `{"labels":`, true), 400)
	expect(request("PUT", "/api/v1/applications/app/favorite", "viewer", `{"unknown":true}`, true), 400)
	expect(request("POST", "/api/v1/application-views", "viewer", `{`, true), 400)
	expect(request("PUT", "/api/v1/application-views/default", "viewer", `{`, true), 400)
	labels := `{"labels":{"team":"platform","environment":"production"}}`
	expect(request("PUT", "/api/v1/applications/app/labels", "owner", labels, false), 403)
	expect(request("PUT", "/api/v1/applications/app/labels", "viewer", labels, true), 403)
	expect(request("PUT", "/api/v1/applications/app/labels", "owner", `{"labels":{"bad key":"x"}}`, true), 400)
	expect(request("PUT", "/api/v1/applications/app/labels", "owner", labels, true), 200)
	app, err := db.ApplicationByID(ctx, "app")
	if err != nil || app.Labels["team"] != "platform" {
		t.Fatalf("labels did not persist: %+v %v", app, err)
	}
	expect(request("PUT", "/api/v1/applications/app/favorite", "other", `{"favorite":true}`, true), 403)
	expect(request("PUT", "/api/v1/applications/app/favorite", "viewer", `{"favorite":true}`, false), 403)
	expect(request("PUT", "/api/v1/applications/app/favorite", "viewer", `{"favorite":true}`, true), 200)
	expect(request("PUT", "/api/v1/applications/app/favorite", "viewer", `{"favorite":true}`, true), 200)
	favorites, err := db.ApplicationFavorites(ctx, store.User{ID: "viewer"})
	if err != nil || len(favorites) != 1 {
		t.Fatalf("favorites not idempotent: %v %v", favorites, err)
	}
	ownersFavorites, err := db.ApplicationFavorites(ctx, store.User{ID: "owner"})
	if err != nil || len(ownersFavorites) != 0 {
		t.Fatal("favorites leaked between users")
	}
	viewJSON := `{"name":"Production by team","configuration":{"groupBy":"label","labelKey":"team","query":"","status":"all","clusterId":"","layout":"cards","sort":"name","favoritesOnly":true,"filters":{"id":"root","type":"group","combinator":"and","rules":[]}}}`
	created := request("POST", "/api/v1/application-views", "viewer", viewJSON, true)
	expect(created, 201)
	var view store.ApplicationSavedView
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	response := request("GET", "/api/v1/application-preferences", "viewer", "", false)
	expect(response, 200)
	var preferences struct {
		Views     []store.ApplicationSavedView `json:"views"`
		Favorites []string                     `json:"favoriteApplicationIds"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preferences); err != nil {
		t.Fatal(err)
	}
	if len(preferences.Views) != 1 || !preferences.Views[0].IsDefault || preferences.Views[0].Configuration.LabelKey != "team" || len(preferences.Favorites) != 1 {
		t.Fatalf("preferences did not round-trip: %+v", preferences)
	}
	expect(request("POST", "/api/v1/application-views", "viewer", viewJSON, true), 409)
	expect(request("PUT", "/api/v1/application-views/"+view.ID, "owner", viewJSON, true), 404)
	expect(request("DELETE", "/api/v1/application-views/"+view.ID, "owner", "", true), 404)
	expect(request("PUT", "/api/v1/application-views/default", "owner", `{"viewId":"`+view.ID+`"}`, true), 404)
	otherPreferences := request("GET", "/api/v1/application-preferences", "owner", "", false)
	expect(otherPreferences, 200)
	if strings.Contains(otherPreferences.Body.String(), view.ID) {
		t.Fatal("saved view leaked to another user")
	}
	expect(request("PUT", "/api/v1/application-views/default", "viewer", `{"viewId":""}`, true), 200)
	expect(request("PUT", "/api/v1/application-views/default", "viewer", `{"viewId":"`+view.ID+`"}`, true), 200)
	expect(request("PUT", "/api/v1/application-views/"+view.ID, "viewer", strings.Replace(viewJSON, `"layout":"cards"`, `"layout":"list"`, 1), true), 200)
	expect(request("DELETE", "/api/v1/application-views/"+view.ID, "viewer", "", true), 200)
	expect(request("PUT", "/api/v1/applications/app/favorite", "viewer", `{"favorite":false}`, true), 200)
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO repository_configurations(id,workspace_id,source_id,revision) VALUES('repo','w','source','main'); UPDATE applications SET repository_configuration_id='repo' WHERE id='app'`); err != nil {
		t.Fatal(err)
	}
	expect(request("PUT", "/api/v1/applications/app/labels", "owner", labels, true), 409)
}
