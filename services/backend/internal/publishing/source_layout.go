package publishing

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// NormalizeSource accepts root-level projects and a single enclosing folder.
// It never searches arbitrary subdirectories or chooses between multiple apps.
func NormalizeSource(c Config, files []File) ([]File, string, error) {
	if c.EntryPoint == "" || !safePath(c.EntryPoint) || !filePattern.MatchString(c.EntryPoint) {
		return nil, "", errors.New("select a valid relative application entry point")
	}
	if err := checkSourceLayout(c, files); err == nil {
		return files, "", nil
	}
	prefix := ""
	for _, f := range files {
		relative := strings.TrimPrefix(f.Path, "source/")
		first, _, ok := strings.Cut(relative, "/")
		if !ok || (prefix != "" && prefix != first) {
			return nil, "", checkSourceLayout(c, files)
		}
		prefix = first
	}
	if prefix != "" {
		normalized := append([]File(nil), files...)
		for i := range normalized {
			normalized[i].Path = "source/" + strings.TrimPrefix(normalized[i].Path, "source/"+prefix+"/")
		}
		if err := checkSourceLayout(c, normalized); err == nil {
			return normalized, prefix, nil
		}
	}
	return nil, "", checkSourceLayout(c, files)
}

func checkSourceLayout(c Config, files []File) error {
	has := func(p string) bool {
		for _, f := range files {
			if f.Path == "source/"+p {
				return true
			}
		}
		return false
	}
	if c.Layout != nil && len(c.Layout.Rules) > 0 {
		if len(files) == 0 {
			return errors.New("archive contains no application files")
		}
		return c.Layout.check(c.EntryPoint, has)
	}
	switch c.Runtime {
	case RuntimeServer:
		if len(files) == 0 {
			return errors.New("archive contains no application files")
		}
	case "shiny":
		if !has(path.Join(c.EntryPoint, "app.R")) && !(has(path.Join(c.EntryPoint, "ui.R")) && has(path.Join(c.EntryPoint, "server.R"))) {
			return fmt.Errorf("Shiny needs app.R, or both ui.R and server.R, in app directory %q. Put the project contents at the ZIP root, or choose the correct app directory", c.EntryPoint)
		}
		if !has("renv.lock") && !has("packages.txt") {
			return errors.New("Shiny needs renv.lock or packages.txt at the project root (outside a nested app directory)")
		}
	case "streamlit":
		if !has(c.EntryPoint) {
			return fmt.Errorf("Streamlit entry file %q is missing. Put it at the ZIP root or select its relative path, for example src/app.py", c.EntryPoint)
		}
		if !has("requirements.txt") {
			return errors.New("Streamlit needs requirements.txt at the project root, even when the entry file is in a subdirectory")
		}
	case "static":
		if !has(path.Join(c.EntryPoint, "index.html")) {
			return fmt.Errorf("Static website directory %q needs index.html. Put index.html at the ZIP root or select the website directory, for example public", c.EntryPoint)
		}
	case "python-static":
		if len(files) == 0 {
			return errors.New("archive contains no application files")
		}
	default:
		return errors.New("unsupported application runtime")
	}
	return nil
}
