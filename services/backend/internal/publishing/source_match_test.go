package publishing

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

func TestSourceRetryVerifiesActualGitContents(t *testing.T) {
	old := "<p>Old HTML</p>"
	fresh := "<p>New HTML</p>"
	hash := func(s string) string {
		return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(s), s))))
	}
	for _, tc := range []struct {
		name    string
		content string
		extra   bool
		want    bool
	}{
		{"unchanged", old, false, true}, {"new upload", fresh, false, false}, {"removed source file", old, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := dir + "/index.html"
			if err := os.WriteFile(file, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				tree := []map[string]string{{"path": "source/index.html", "type": "blob", "id": hash(old)}, {"path": "deploy/application.yaml", "type": "blob", "id": "irrelevant"}}
				if tc.extra {
					tree = append(tree, map[string]string{"path": "source/removed.html", "type": "blob", "id": hash("removed")})
				}
				b, _ := json.Marshal(tree)
				return response(200, string(b)), nil
			})}}
			matches, err := g.SourceMatches(context.Background(), Project{ID: 1}, []File{{Path: "source/index.html", DiskPath: file, Encoding: "base64"}, {Path: "Dockerfile", Content: "irrelevant"}})
			if err != nil || matches != tc.want {
				t.Fatal(matches, err)
			}
		})
	}
}
