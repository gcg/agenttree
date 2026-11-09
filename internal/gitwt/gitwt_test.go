package gitwt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	run("config", "tag.gpgsign", "false")
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644)
	run("add", "README.md")
	run("commit", "-m", "initial")
	return dir
}

func TestSlugify(t *testing.T) {
	slug, branch, err := Slugify("Add User Auth!", "feat/")
	if err != nil || slug != "add-user-auth" || branch != "feat/add-user-auth" {
		t.Fatalf("got slug=%q branch=%q err=%v", slug, branch, err)
	}
	if _, _, err := Slugify("", ""); err == nil {
		t.Fatalf("expected empty slug to fail")
	}
}

func TestEnsureIgnoredMergeFallback(t *testing.T) {
	repo := initTempRepo(t)
	_, commonDir, err := ResolveRepo(repo)
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	// Simulate in-progress merge via MERGE_HEAD (Review Focus #2).
	_ = os.WriteFile(filepath.Join(commonDir, "MERGE_HEAD"), []byte("deadbeef\n"), 0o644)
	applied, err := EnsureIgnored(repo, commonDir, ".worktrees", "gitignore-commit")
	if err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	if applied != "gitignore" {
		t.Fatalf("expected fallback to gitignore during merge, got %q", applied)
	}
}

func TestWorktreeLifecycleAndResolveFromInsideWorktree(t *testing.T) {
	repo := initTempRepo(t)
	wtRoot := filepath.Join(repo, ".worktrees")
	wtPath := filepath.Join(wtRoot, "feat-a")

	if err := AddWorktree(repo, wtPath, "feat-a", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// Review Focus #1: ResolveRepo from inside a worktree returns main repo root.
	resolvedRoot, _, err := ResolveRepo(wtPath)
	if err != nil {
		t.Fatalf("ResolveRepo from worktree: %v", err)
	}
	realRepo, _ := filepath.EvalSymlinks(repo)
	realResolved, _ := filepath.EvalSymlinks(resolvedRoot)
	if realResolved != realRepo {
		t.Fatalf("expected %q, got %q", realRepo, realResolved)
	}

	// ListWorktrees should include feat-a
	list, err := ListWorktrees(repo, wtRoot)
	if err != nil || len(list) != 1 || list[0].Branch != "feat-a" {
		t.Fatalf("ListWorktrees: got %v err=%v", list, err)
	}

	// Make an unmerged commit in the worktree, then verify prMerged=true clears Unmerged (Review Focus #3).
	_ = os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("x"), 0o644)
	for _, args := range [][]string{{"add", "new.txt"}, {"commit", "-m", "wip"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wtPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	repUnmerged, err := CheckSafety(repo, wtPath, "feat-a", false)
	if err != nil || !repUnmerged.Unmerged {
		t.Fatalf("expected Unmerged=true when prMerged=false, got %+v err=%v", repUnmerged, err)
	}
	if !repUnmerged.NeedsForce() {
		t.Fatalf("expected NeedsForce=true when unmerged")
	}

	repSquashMerged, err := CheckSafety(repo, wtPath, "feat-a", true)
	if err != nil || repSquashMerged.Unmerged {
		t.Fatalf("expected Unmerged=false when prMerged=true, got %+v err=%v", repSquashMerged, err)
	}

	if err := RemoveWorktree(repo, wtPath, "feat-a", true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
}
