package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/store"
	"sigs.k8s.io/yaml"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil { panic(err) }
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil { panic(err) }
	defer db.DB.Close()
	app, err := db.ApplicationByID(ctx, "bd9de534b2afbbecb9944458cfbb46df")
	if err != nil { panic(err) }
	source, err := db.GitSourceByID(ctx, app.SourceID)
	if err != nil { panic(err) }
	checkout, err := gitops.Fetch(ctx, db, cfg.EncryptionKey, source, app.Revision)
	if err != nil { panic(err) }
	defer checkout.Close()
	fmt.Printf("commit=%s manifestPath=%s\n", checkout.Commit[:12], app.ManifestPath)
	_ = filepath.WalkDir(checkout.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil { return walkErr }
		if entry.IsDir() && entry.Name() == ".git" { return filepath.SkipDir }
		if entry.IsDir() || !strings.HasPrefix(strings.ToLower(entry.Name()), "kustomization.") { return nil }
		content, err := os.ReadFile(path)
		if err != nil { return err }
		var data map[string]any
		if err := yaml.Unmarshal(content, &data); err != nil { return err }
		for _, field := range []string{"resources", "bases", "components"} {
			values, _ := data[field].([]any)
			for _, value := range values {
				local, ok := value.(string)
				if !ok { continue }
				target := filepath.Join(filepath.Dir(path), local)
				rel, err := filepath.Rel(checkout.Root, target)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					fmt.Printf("ESCAPE file=%s field=%s value=%q\n", strings.TrimPrefix(path, checkout.Root+string(filepath.Separator)), field, local)
				}
			}
		}
		return nil
	})
}
