package publishing

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// LayoutRule requires an uploaded project to contain files. The rule passes
// when any one group of AnyOf has all of its files. Paths are relative to the
// project root; {entry} stands for the application's entry point.
type LayoutRule struct {
	Message string     `json:"message,omitempty"`
	AnyOf   [][]string `json:"anyOf"`
}

// TemplateLayout is the resolved layout of a configuration. It is a pointer in
// Config so configurations stay comparable.
type TemplateLayout struct{ Rules []LayoutRule }

// Runtimes a template can run as. "server" is the generic runtime of custom
// templates: a base image, a start command in the Dockerfile and a port.
const RuntimeServer = "server"

// RuntimeForKind maps the choices offered when creating a template to the
// runtime that drives validation and rendering.
func RuntimeForKind(kind string) (string, bool) {
	switch kind {
	case "server":
		return RuntimeServer, true
	case "static":
		return "static", true
	case "static-build":
		return "python-static", true
	}
	return "", false
}

func validRuntime(runtime string) bool {
	switch runtime {
	case "shiny", "streamlit", "python-static", "static", RuntimeServer:
		return true
	}
	return false
}

// IsStaticRuntime reports whether the runtime is served by a static web server.
func IsStaticRuntime(runtime string) bool { return runtime == "static" || runtime == "python-static" }

var customTemplateID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// ValidateCustomTemplate checks the definition of a workspace template.
func ValidateCustomTemplate(t Template) error {
	if !customTemplateID.MatchString(t.ID) {
		return errors.New("template ID must be 3 to 40 lowercase letters, numbers and hyphens, starting with a letter")
	}
	if _, builtin := BuiltinLookup(t.ID); builtin {
		return errors.New("template ID is reserved for a built-in template")
	}
	name := strings.TrimSpace(t.Name)
	if name == "" || len(name) > 80 {
		return errors.New("template name is required (maximum 80 characters)")
	}
	if len(t.Description) > 400 || len(t.Detail) > 120 || len(t.Category) > 40 {
		return errors.New("description (400), detail (120) and category (40) are too long")
	}
	if !validRuntime(t.Runtime) {
		return errors.New("unsupported template kind")
	}
	if !safePath(t.EntryPoint) || !filePattern.MatchString(t.EntryPoint) {
		return errors.New("entry point must be a relative application path without spaces")
	}
	if t.Port < 0 || t.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return ValidateLayout(t.Layout)
}

// ValidateLayout checks layout rules for size and safe paths.
func ValidateLayout(rules []LayoutRule) error {
	if len(rules) > 10 {
		return errors.New("at most 10 upload rules are allowed")
	}
	for i, rule := range rules {
		if len(rule.Message) > 300 {
			return fmt.Errorf("upload rule %d: message is too long", i+1)
		}
		if len(rule.AnyOf) == 0 || len(rule.AnyOf) > 5 {
			return fmt.Errorf("upload rule %d needs one to five alternatives", i+1)
		}
		for _, group := range rule.AnyOf {
			if len(group) == 0 || len(group) > 5 {
				return fmt.Errorf("upload rule %d: each alternative needs one to five files", i+1)
			}
			for _, p := range group {
				check := strings.ReplaceAll(p, "{entry}", "entry")
				if !safePath(check) || !filePattern.MatchString(check) || strings.HasSuffix(check, "/") {
					return fmt.Errorf("upload rule %d: %q is not a safe relative file path", i+1, p)
				}
			}
		}
	}
	return nil
}

func (l *TemplateLayout) check(entry string, has func(string) bool) error {
	for _, rule := range l.Rules {
		ok := false
		var wanted []string
		for _, group := range rule.AnyOf {
			all := true
			for _, p := range group {
				if !has(path.Join(strings.ReplaceAll(p, "{entry}", entry))) {
					all = false
				}
			}
			if all {
				ok = true
				break
			}
			wanted = append(wanted, strings.Join(group, " + "))
		}
		if !ok {
			if rule.Message != "" {
				return errors.New(rule.Message)
			}
			return fmt.Errorf("the ZIP is missing required files: %s", strings.Join(wanted, " or "))
		}
	}
	return nil
}
