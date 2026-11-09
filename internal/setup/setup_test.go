package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcg/agenttree/internal/config"
)

func TestDetectAndRunCopyNeverOverwrites(t *testing.T) {
	repo := t.TempDir()
	wt := t.TempDir()

	_ = os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL=1"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("lock"), 0o644)

	// Pre-populate .env in worktree to verify it is NOT overwritten (§6.11).
	_ = os.WriteFile(filepath.Join(wt, ".env"), []byte("EXISTING=1"), 0o644)

	cands, err := DetectCopyCandidates(repo)
	if err != nil || len(cands) != 2 {
		t.Fatalf("DetectCopyCandidates: got %v, err=%v", cands, err)
	}
	dep := DetectDeps(repo)
	if dep.InstallCmd != "pnpm install" || dep.CopyDir != "node_modules" {
		t.Fatalf("unexpected DepInfo: %+v", dep)
	}

	var buf bytes.Buffer
	cfg := config.SetupConfig{
		Copy:       []string{".env", ".env.local"},
		Deps:       "install",
		InstallCmd: "echo installed-ok",
	}
	if err := Run(repo, wt, cfg, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	envBytes, _ := os.ReadFile(filepath.Join(wt, ".env"))
	if string(envBytes) != "EXISTING=1" {
		t.Fatalf(".env in worktree was overwritten: %q", string(envBytes))
	}
	localBytes, _ := os.ReadFile(filepath.Join(wt, ".env.local"))
	if string(localBytes) != "LOCAL=1" {
		t.Fatalf("expected .env.local to be copied, got %q", string(localBytes))
	}
}
