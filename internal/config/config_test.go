package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultAndMergeOverride(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.toml")
	repoPath := filepath.Join(dir, "repo.toml")

	// Missing files return defaults and repoExists=false.
	cfg, repoExists, err := Load(globalPath, repoPath)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if repoExists {
		t.Fatalf("expected repoExists=false")
	}
	if cfg.Layout != "sidebar" || cfg.SidebarWidth != 24 || cfg.DefaultAgent != "claude" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Agents["codex"].Resume != "codex resume --last" {
		t.Fatalf("unexpected codex resume: %q", cfg.Agents["codex"].Resume)
	}

	// Save global with layout="tabs", repo with worktree_dir=".trees" and default_agent="agy".
	g := Default()
	g.Layout = "tabs"
	if err := Save(globalPath, g); err != nil {
		t.Fatalf("Save global: %v", err)
	}
	r := Default()
	r.Layout = "tabs"
	r.WorktreeDir = ".trees"
	r.DefaultAgent = "agy"
	if err := Save(repoPath, r); err != nil {
		t.Fatalf("Save repo: %v", err)
	}

	merged, repoExists, err := Load(globalPath, repoPath)
	if err != nil {
		t.Fatalf("Load merged: %v", err)
	}
	if !repoExists || merged.Layout != "tabs" || merged.WorktreeDir != ".trees" || merged.DefaultAgent != "agy" {
		t.Fatalf("unexpected merged config: %+v (repoExists=%v)", merged, repoExists)
	}
}
