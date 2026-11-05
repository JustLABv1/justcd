package publishing

import (
	"strings"
	"testing"
)

func TestSourceLayoutNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, runtime, entry string
		paths                []string
		root                 string
		valid                bool
	}{
		{"static root", "static", ".", []string{"index.html", "css/main.css"}, "", true},
		{"static folder", "static", ".", []string{"site/index.html", "site/css/main.css"}, "site", true},
		{"explicit static directory", "static", "public", []string{"public/index.html", "README.md"}, "", true},
		{"shiny folder", "shiny", ".", []string{"project/app.R", "project/packages.txt"}, "project", true},
		{"nested shiny app", "shiny", "src/dashboard", []string{"project/src/dashboard/ui.R", "project/src/dashboard/server.R", "project/renv.lock"}, "project", true},
		{"streamlit folder", "streamlit", "app.py", []string{"project/app.py", "project/requirements.txt"}, "project", true},
		{"nested streamlit entry", "streamlit", "src/app.py", []string{"src/app.py", "requirements.txt"}, "", true},
		{"missing dependencies", "streamlit", "app.py", []string{"project/app.py"}, "", false},
		{"wrong case", "static", ".", []string{"Index.html"}, "", false},
		{"ambiguous apps", "static", ".", []string{"a/index.html", "b/index.html"}, "", false},
		{"shiny missing server", "shiny", ".", []string{"ui.R", "packages.txt"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []File{}
			for _, p := range tc.paths {
				files = append(files, File{Path: "source/" + p, Content: p, Encoding: "text"})
			}
			before := files[0].Path
			got, root, err := NormalizeSource(Config{Runtime: tc.runtime, EntryPoint: tc.entry}, files)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid {
				if root != tc.root {
					t.Fatalf("root=%q", root)
				}
				if root != "" && got[0].Path != "source/"+strings.TrimPrefix(tc.paths[0], root+"/") {
					t.Fatal(got)
				}
				if files[0].Path != before {
					t.Fatal("mutated original file list")
				}
			}
		})
	}
}

func TestWrappedStaticSourceReachesGeneratedDockerContext(t *testing.T) {
	c, err := SampleConfig("static")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(c, []File{{Path: "source/website/index.html", Content: "<p>new version</p>", Encoding: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range generated {
		if f.Path == "source/index.html" && f.Content == "<p>new version</p>" {
			found = true
		}
	}
	if !found {
		t.Fatal("wrapper was not removed in the actual publishing path")
	}
}
