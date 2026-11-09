# agenttree Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `agenttree`, a tmux-backed Go TUI that manages one git worktree and one live coding agent per feature, with live activity status, PR status, worktree setup, and a `lazygit` popup.

**Architecture:** `agenttree` runs in a narrow sidebar or top tab-bar pane inside the user's existing tmux server and splits an adjacent "agent slot" pane. Actual agent processes live in a detached per-repo background tmux session (`_agenttree-<repo>-<hash>`) and are swapped into the visible slot via `tmux swap-pane`. Global tmux keybindings and agent lifecycle hooks talk back to the running `agenttree` UI over a per-repo Unix domain socket (`<git-common-dir>/agenttree/ctl.sock`).

**Tech Stack:** Go ≥ 1.24, Charm (`github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/huh`), TOML (`github.com/BurntSushi/toml`), tmux ≥ 3.2, git ≥ 2.20, optional `gh` and `lazygit`.

**Spec:** [docs/superpowers/specs/2026-11-09-agenttree-design.md](../specs/2026-11-09-agenttree-design.md)

## Global Constraints

- **PR size rule (`AGENTS.md`):** One logical change per PR (1 Task below = 1 PR). Target under 300 changed non-test lines; hard limit 500. Each task must compile (`go build ./...`) and pass tests (`go test ./...`) on its own.
- **Runtime requirements:** Go ≥ 1.24, tmux ≥ 3.2, git ≥ 2.20, `gh` optional, `lazygit` optional.
- **Never start a separate tmux server:** Always use the server in `$TMUX`. If `$TMUX` is empty when launching the UI, print `"agenttree must be started inside tmux ($TMUX is not set)"` and exit non-zero.
- **Background session naming:** `_agenttree-<repo-name>-<4-char-hex-sha256-of-repo-root>`.
- **Default paths:** Global config at `~/.config/agenttree/config.toml`; per-repo config at `<git-common-dir>/agenttree/config.toml`; state at `<git-common-dir>/agenttree/state.json`; lock at `<git-common-dir>/agenttree/lock`; socket at `<git-common-dir>/agenttree/ctl.sock`; default worktree folder `.worktrees`.
- **Supported agents & default commands:**
  - `claude`: launch `"claude"`, resume `"claude --continue"`
  - `agy`: launch `"agy"`, resume `"agy --continue"`
  - `codex`: launch `"codex"`, resume `"codex resume --last"`
  - `opencode`: launch `"opencode"`, resume `"opencode --continue"`
- **No clobbering user tmux bindings:** Before `tmux bind-key`, check `tmux list-keys`; if the key is already bound (and not to `agenttree ctl`), skip binding it and surface a warning in settings.
- **Never overwrite existing worktree files during setup copy:** If `<worktree>/<file>` already exists (e.g. tracked in git), skip copying it.

## PR Split Plan (9 Stacked PRs, each < 300 non-test LoC)

| PR / Task | Scope | Est. Non-Test LoC | Depends On |
|---|---|---|---|
| **PR 1 (Task 1)** | Go module scaffold + TOML Config load/merge/save (`internal/config`) | ~170 | — |
| **PR 2 (Task 2)** | State file persistence + single-instance lock (`internal/state`) | ~150 | PR 1 |
| **PR 3 (Task 3)** | Git & worktree operations, ignore modes, safety checks (`internal/gitwt`) | ~240 | PR 1 |
| **PR 4 (Task 4)** | `Multiplexer` interface & `tmux` backend (`internal/mux`) | ~260 | PR 1 |
| **PR 5 (Task 5)** | Activity state machine (`internal/activity`) & Agent hook installer (`internal/hooks`) | ~230 | PR 1 |
| **PR 6 (Task 6)** | Unix control socket (`internal/ctl`) & GitHub PR poller (`internal/pr`) | ~210 | PR 1 |
| **PR 7 (Task 7)** | Worktree setup detection, CoW copy, and dependency install (`internal/setup`) | ~190 | PR 1 |
| **PR 8 (Task 8)** | Bubble Tea UI: sidebar/tabs, pane switching, restart/resume, `lazygit` popup (`internal/ui`) | ~280 | PRs 1–7 |
| **PR 9 (Task 9)** | Huh popups (onboarding, new, setup, cleanup, settings) + `cmd/agenttree` CLI wiring | ~270 | PR 8 |

## Review Focus

1. **Started inside an existing git worktree instead of the main checkout:** `ResolveRepo` must use `git rev-parse --path-format=absolute --git-common-dir` to find the shared `.git` dir and derive the main checkout root (`filepath.Dir(gitCommonDir)`), not `git rev-parse --show-toplevel` (which returns the worktree's own root). Tested in Task 3.
2. **Main checkout is mid-merge or mid-rebase during onboarding with `gitignore-commit`:** `EnsureIgnored` must detect `MERGE_HEAD`, `rebase-merge`, or `rebase-apply` inside `gitCommonDir`, fall back to `gitignore` (append without committing), and return `"gitignore"` as the applied mode instead of failing or committing partial merge state. Tested in Task 3.
3. **Squash-merged PRs look unmerged to `git branch --merged`:** `CheckSafety` must treat a branch as merged when `prMerged == true` even if git reports the commits are not ancestors of `HEAD`. Tested in Task 3.
4. **User already bound one of our default tmux keys (e.g. `prefix + S`):** `TmuxMux.BindKey` must inspect `tmux list-keys -T <table>` first and return `skipped = true, err = nil` without overwriting a non-`agenttree` binding. Tested in Task 4.
5. **Stale lock file left behind after a crash (`SIGKILL`):** `AcquireLock` must check if the recorded PID is still alive (`kill(pid, 0)`) and, if dead, overwrite the stale lock file instead of refusing startup. Tested in Task 2.

---

### Task 1 (PR 1): Go Module & TOML Configuration (`internal/config`)

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package config`):
  ```go
  type AgentCmd struct {
      Launch string `toml:"launch"`
      Resume string `toml:"resume"`
  }
  type KeysConfig struct {
      New      string `toml:"new"`       // default "C"
      Next     string `toml:"next"`      // default ")"
      Prev     string `toml:"prev"`      // default "("
      Lazygit  string `toml:"lazygit"`   // default "G"
      Cleanup  string `toml:"cleanup"`   // default "X"
      Restart  string `toml:"restart"`   // default "R"
      Settings string `toml:"settings"`  // default "S"
      AltJump  bool   `toml:"alt_jump"`  // default true (M-1..M-9 in root table)
  }
  type StatusConfig struct {
      WorkingTimeoutSec int `toml:"working_timeout"` // default 3
  }
  type PRConfig struct {
      PollIntervalSec int `toml:"poll_interval"` // default 60
  }
  type SetupConfig struct {
      Copy       []string `toml:"copy"`        // e.g. [".env", ".env.*"]
      Deps       string   `toml:"deps"`        // "none" | "copy" | "install"
      InstallCmd string   `toml:"install_cmd"` // e.g. "npm ci"
      Configured bool     `toml:"configured"`  // true once first-feature popup answered
  }
  type Config struct {
      Layout       string              `toml:"layout"`        // "sidebar" | "tabs" (default "sidebar")
      SidebarWidth int                 `toml:"sidebar_width"` // default 24
      DefaultAgent string              `toml:"default_agent"` // default "claude"
      BranchPrefix string              `toml:"branch_prefix"` // default ""
      WorktreeDir  string              `toml:"worktree_dir"`  // default ".worktrees"
      IgnoreMode   string              `toml:"ignore_mode"`   // "gitignore-commit" | "gitignore" | "exclude"
      Agents       map[string]AgentCmd `toml:"agents"`
      Keys         KeysConfig          `toml:"keys"`
      Status       StatusConfig        `toml:"status"`
      PR           PRConfig            `toml:"pr"`
      Setup        SetupConfig         `toml:"setup"`
  }
  func Default() Config
  func Load(globalPath, repoPath string) (merged Config, repoExists bool, err error)
  func Save(path string, cfg Config) error
  func GlobalConfigPath() string
  func RepoConfigPath(gitCommonDir string) string
  ```

- [ ] **Step 1: Initialize `go.mod` and write failing config tests**

Run `go mod init github.com/gcg/agenttree && go get github.com/BurntSushi/toml@latest`. Create `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config -v`
Expected: FAIL (`undefined: Load`)

- [ ] **Step 3: Implement `internal/config/config.go`**

Implement `Default()`, `Load(globalPath, repoPath string)`, `Save(path string, cfg Config)`, `GlobalConfigPath()`, and `RepoConfigPath(gitCommonDir string)` in `internal/config/config.go`. In `Load`, start with `cfg := Default()`, decode `globalPath` into `&cfg` if the file exists, then decode `repoPath` into `&cfg` if the file exists (setting `repoExists = true`), ensuring any unset entries in `cfg.Agents` keep their defaults from `Default()`. `Save` creates parent directories (`os.MkdirAll(filepath.Dir(path), 0o755)`) and writes TOML atomically via a temp file + `os.Rename`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): add TOML config defaults, merging, and atomic save"
```

---

### Task 2 (PR 2): State File Persistence & Instance Lock (`internal/state`)

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package state`):
  ```go
  type Feature struct {
      Name         string    `json:"name"`
      Branch       string    `json:"branch"`
      WorktreePath string    `json:"worktree_path"`
      AgentID      string    `json:"agent_id"`
      CreatedAt    time.Time `json:"created_at"`
  }
  type State struct {
      Features        []Feature `json:"features"`
      SelectedFeature string    `json:"selected_feature"`
  }
  type LockInfo struct {
      PID    int    `json:"pid"`
      PaneID string `json:"pane_id"`
  }
  type LockHeldError struct {
      Holder LockInfo
  }
  func (e *LockHeldError) Error() string
  func StatePath(gitCommonDir string) string
  func Load(gitCommonDir string) (*State, error)
  func Save(gitCommonDir string, st *State) error
  func (s *State) Upsert(f Feature)
  func (s *State) Remove(name string)
  func (s *State) Find(name string) (Feature, bool)
  func AcquireLock(gitCommonDir, paneID string) (release func() error, err error)
  ```

- [ ] **Step 1: Write failing state and lock tests (including stale-PID recovery from Review Focus #5)**

Create `internal/state/state_test.go`:

```go
package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateLoadSaveUpsertRemove(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	f := Feature{Name: "auth", Branch: "auth", WorktreePath: "/tmp/wt/auth", AgentID: "claude", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	st.Upsert(f)
	st.SelectedFeature = "auth"
	if err := Save(dir, st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load saved: %v", err)
	}
	if loaded.SelectedFeature != "auth" || len(loaded.Features) != 1 || loaded.Features[0].Name != "auth" {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}
	loaded.Remove("auth")
	if len(loaded.Features) != 0 {
		t.Fatalf("expected empty features after Remove")
	}
}

func TestAcquireLockAndStalePIDRecovery(t *testing.T) {
	dir := t.TempDir()
	// Write a stale lock with a dead PID (99999999).
	lockDir := filepath.Join(dir, "agenttree")
	_ = os.MkdirAll(lockDir, 0o755)
	_ = os.WriteFile(filepath.Join(lockDir, "lock"), []byte(`{"pid":99999999,"pane_id":"%99"}`), 0o644)

	rel, err := AcquireLock(dir, "%1")
	if err != nil {
		t.Fatalf("expected stale lock to be replaced, got: %v", err)
	}
	defer rel()

	// Second AcquireLock while current process is alive must fail with *LockHeldError.
	_, err = AcquireLock(dir, "%2")
	var held *LockHeldError
	if !errors.As(err, &held) || held.Holder.PaneID != "%1" {
		t.Fatalf("expected LockHeldError for pane %%1, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state -v`
Expected: FAIL (`undefined: Load`)

- [ ] **Step 3: Implement `internal/state/state.go`**

Implement `Load`, `Save` (atomic write via temp file in `<git-common-dir>/agenttree/state.json`), `Upsert`, `Remove`, `Find`, and `AcquireLock`. In `AcquireLock`, check if `<git-common-dir>/agenttree/lock` exists: if it decodes to a `LockInfo` whose `PID > 0` and `syscall.Kill(info.PID, 0) == nil`, return `&LockHeldError{Holder: info}`. Otherwise write `{PID: os.Getpid(), PaneID: paneID}` and return a `release` closure that removes the lock file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat(state): add state.json persistence and stale-aware instance lock"
```

---

### Task 3 (PR 3): Git & Worktree Operations (`internal/gitwt`)

**Files:**
- Create: `internal/gitwt/gitwt.go`
- Test: `internal/gitwt/gitwt_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package gitwt`):
  ```go
  type WorktreeInfo struct {
      Path   string
      Branch string
  }
  type SafetyReport struct {
      Dirty    bool
      Unpushed bool
      Unmerged bool
  }
  func (r SafetyReport) NeedsForce() bool // Dirty || Unpushed || Unmerged
  func ResolveRepo(cwd string) (repoRoot, gitCommonDir string, err error)
  func Slugify(name, branchPrefix string) (slug, branch string, err error)
  func DefaultBranch(repoRoot string) string
  func EnsureIgnored(repoRoot, gitCommonDir, worktreeDir, mode string) (appliedMode string, err error)
  func AddWorktree(repoRoot, worktreePath, branch, baseRef string) error
  func ListWorktrees(repoRoot, worktreesRoot string) ([]WorktreeInfo, error)
  func CheckSafety(repoRoot, worktreePath, branch string, prMerged bool) (SafetyReport, error)
  func RemoveWorktree(repoRoot, worktreePath, branch string, force bool) error
  ```

- [ ] **Step 1: Write failing gitwt tests (covering Review Focus #1, #2, and #3)**

Create `internal/gitwt/gitwt_test.go`:

```go
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
	repSquashMerged, err := CheckSafety(repo, wtPath, "feat-a", true)
	if err != nil || repSquashMerged.Unmerged {
		t.Fatalf("expected Unmerged=false when prMerged=true, got %+v err=%v", repSquashMerged, err)
	}

	if err := RemoveWorktree(repo, wtPath, "feat-a", true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitwt -v`
Expected: FAIL (`undefined: ResolveRepo`)

- [ ] **Step 3: Implement `internal/gitwt/gitwt.go`**

Implement all functions in `internal/gitwt/gitwt.go`:
- `ResolveRepo`: run `git -C <cwd> rev-parse --path-format=absolute --git-common-dir` to get `gitCommonDir`, and derive `repoRoot = filepath.Dir(gitCommonDir)`.
- `Slugify`: lowercase `name`, replace any run of characters outside `[a-z0-9._-]` with `-`, trim leading/trailing `-` and `.`, combine `branch = branchPrefix + slug`, and validate with `git check-ref-format --branch <branch>`.
- `DefaultBranch`: run `git -C <repoRoot> symbolic-ref refs/remotes/origin/HEAD` (if found, e.g. `refs/remotes/origin/main`, run `git -C <repoRoot> fetch origin main` ignoring offline errors and return `"origin/main"`); otherwise check if `main` or `master` exists locally, falling back to `"HEAD"`.
- `EnsureIgnored`: first run `git -C <repoRoot> check-ignore -q <worktreeDir>`; if exit 0, return `mode, nil`. If `mode == "gitignore-commit"` and any of `MERGE_HEAD`, `rebase-merge`, or `rebase-apply` exists in `gitCommonDir`, downgrade `mode` to `"gitignore"`. For `gitignore`/`gitignore-commit`, append `/<worktreeDir>/\n` to `<repoRoot>/.gitignore` (and for `gitignore-commit`, run `git -C <repoRoot> commit --only .gitignore -m "chore: ignore .worktrees (agenttree)"`). For `exclude`, append `/<worktreeDir>/\n` to `<gitCommonDir>/info/exclude`.
- `AddWorktree`: check if `refs/heads/<branch>` already exists via `git show-ref --verify --quiet refs/heads/<branch>`; if so run `git worktree add <worktreePath> <branch>`, else `git worktree add -b <branch> <worktreePath> <baseRef>`.
- `ListWorktrees`: parse `git -C <repoRoot> worktree list --porcelain` and filter to paths inside `worktreesRoot`.
- `CheckSafety`: `Dirty` if `git -C <worktreePath> status --porcelain` is non-empty; `Unmerged` if `!prMerged` and `git -C <repoRoot> merge-base --is-ancestor <branch> HEAD` exits non-zero; `Unpushed` if `!prMerged` and upstream exists with `git -C <worktreePath> rev-list @{u}..HEAD --count` > 0.
- `RemoveWorktree`: run `git -C <repoRoot> worktree remove [--force] <worktreePath>` then `git -C <repoRoot> branch [-d|-D] <branch>`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gitwt -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitwt/
git commit -m "feat(gitwt): add git worktree operations, ignore modes, and safety checks"
```

---

### Task 4 (PR 4): Multiplexer Abstraction & tmux Backend (`internal/mux`)

**Files:**
- Create: `internal/mux/mux.go`
- Create: `internal/mux/tmux.go`
- Test: `internal/mux/tmux_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package mux`):
  ```go
  type PaneID string
  type Multiplexer interface {
      EnsureSession(name string) error
      NewAgentPane(session, feature, cwd, cmd string, env map[string]string) (PaneID, error)
      FindPanes(session string) (map[string]PaneID, error) // feature -> pane
      Show(feature PaneID, slot PaneID) error               // swap into slot + focus slot
      Capture(p PaneID) (string, error)
      HasBell(p PaneID) (bool, error)
      IsDead(p PaneID) (bool, error)
      Respawn(p PaneID, cmd string) error
      Kill(p PaneID) error
      Popup(cwd, cmd string, wPct, hPct int) error
      BindKey(table, key, cmd string) (skipped bool, err error)
      UnbindKey(table, key string) error
  }
  type Runner func(args ...string) (string, error)
  type TmuxMux struct {
      Run      Runner
      RepoRoot string
  }
  func NewTmuxMux(repoRoot string) *TmuxMux
  func BackgroundSessionName(repoRoot string) string
  func InTmux() bool
  func (m *TmuxMux) SetupWindowSlot(layout string, size int, placeholderCmd, sockPath string) (selfPane, slotPane PaneID, windowID string, err error)
  func (m *TmuxMux) CleanupWindow(slotPane, activeFeaturePane PaneID, bgSession, windowID string) error
  ```

- [ ] **Step 1: Write failing unit tests for `TmuxMux` using a fake `Runner` (including Review Focus #4)**

Create `internal/mux/tmux_test.go`:

```go
package mux

import (
	"strings"
	"testing"
)

func TestBackgroundSessionName(t *testing.T) {
	got := BackgroundSessionName("/Users/alice/work/my-repo")
	if !strings.HasPrefix(got, "_agenttree-my-repo-") || len(got) != len("_agenttree-my-repo-")+4 {
		t.Fatalf("unexpected session name: %q", got)
	}
}

func TestBindKeySkipsExistingUserBinding(t *testing.T) {
	var calls [][]string
	m := &TmuxMux{
		RepoRoot: "/repo",
		Run: func(args ...string) (string, error) {
			calls = append(calls, args)
			if args[0] == "list-keys" {
				// Simulate user already having prefix + S bound to choose-tree.
				return "bind-key -T prefix S choose-tree\n", nil
			}
			return "", nil
		},
	}
	skipped, err := m.BindKey("prefix", "S", "agenttree ctl settings --window #{window_id}")
	if err != nil || !skipped {
		t.Fatalf("expected skipped=true err=nil, got skipped=%v err=%v", skipped, err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected bind-key not to be called when user binding exists, got %v", calls)
	}
}

func TestShowSwapsAndSelectsSlot(t *testing.T) {
	var calls [][]string
	m := &TmuxMux{
		RepoRoot: "/repo",
		Run: func(args ...string) (string, error) {
			calls = append(calls, args)
			return "", nil
		},
	}
	if err := m.Show("%5", "%2"); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(calls) != 2 || calls[0][0] != "swap-pane" || calls[1][0] != "select-pane" {
		t.Fatalf("unexpected tmux calls: %v", calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mux -v`
Expected: FAIL (`undefined: BackgroundSessionName`)

- [ ] **Step 3: Implement `internal/mux/mux.go` and `internal/mux/tmux.go`**

Implement the interface in `internal/mux/mux.go` and `TmuxMux` in `internal/mux/tmux.go`:
- `BackgroundSessionName(repoRoot)`: `fmt.Sprintf("_agenttree-%s-%x", filepath.Base(repoRoot), sha256.Sum256([]byte(repoRoot))[:2])`.
- `EnsureSession(name)`: runs `has-session -t =<name>`; if non-zero, runs `new-session -d -s <name>`.
- `NewAgentPane(session, feature, cwd, cmd, env)`: runs `new-window -d -t <session>: -n <feature> -c <cwd> -P -F "#{pane_id}" <env flags -e KEY=VAL> <cmd>`, then sets `set-option -p -t <pane> remain-on-exit on`, `set-option -p -t <pane> @agenttree_feature <feature>`, and `set-option -p -t <pane> @agenttree_repo <m.RepoRoot>`.
- `FindPanes(session)`: runs `list-panes -s -t <session> -F "#{pane_id}\t#{@agenttree_feature}"` (plus current window panes) so even a pane currently swapped into the slot is found.
- `Show(feature, slot)`: runs `swap-pane -d -s <feature> -t <slot>` and `select-pane -t <slot>`.
- `BindKey(table, key, cmd)`: runs `list-keys -T <table> <key>`; if output is non-empty and does not contain `"agenttree ctl"`, returns `true, nil`. Otherwise runs `bind-key -T <table> <key> run-shell "<cmd>"`.
- `Popup(cwd, cmd, wPct, hPct)`: runs `display-popup -E -w <wPct>% -h <hPct>% -d <cwd> <cmd>`.
- `SetupWindowSlot`: splits current pane (`split-window -h -b -l <size>` for `sidebar` or `split-window -v -b -l <size>` for `tabs` so `agenttree` stays in the narrow/top pane and the new pane is the agent slot), tags window with `set-option -w @agenttree_ctl <sockPath>`.
- `CleanupWindow`: swaps any active feature pane back to `bgSession`, kills the slot pane, clears `@agenttree_ctl` on the window, and if no other window in the server has `@agenttree_ctl` (`list-windows -a -F "#{@agenttree_ctl}"`), unbinds the global keys.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mux -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mux/
git commit -m "feat(mux): add Multiplexer interface and tmux implementation"
```

---

### Task 5 (PR 5): Activity State Machine & Agent Hooks (`internal/activity`, `internal/hooks`)

**Files:**
- Create: `internal/activity/tracker.go`
- Create: `internal/hooks/hooks.go`
- Test: `internal/activity/tracker_test.go`
- Test: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package activity` & `package hooks`):
  ```go
  // package activity
  type State string
  const (
      StateIdle    State = "idle"
      StateWorking State = "working"
      StateBlocked State = "blocked"
      StateDone    State = "done"
      StateStopped State = "stopped"
  )
  type Tracker struct { /* clock, workingTimeout, per-feature state */ }
  func NewTracker(workingTimeout time.Duration, clock func() time.Time) *Tracker
  func (t *Tracker) OnHook(feature, event string, isViewed bool) State
  func (t *Tracker) OnPoll(feature, screenText string, isDead, hasBell, isViewed bool) State
  func (t *Tracker) MarkViewed(feature string) State
  func (t *Tracker) State(feature string) State

  // package hooks
  func InstallWorktreeHooks(agentID, worktreePath, gitCommonDir, agenttreeBin string) error
  func WrapAgentCommand(agentID, baseCmd, agenttreeBin string) string
  ```

- [ ] **Step 1: Write failing tests for activity state machine and Claude/Codex hook installation**

Create `internal/activity/tracker_test.go` and `internal/hooks/hooks_test.go`:

```go
// internal/activity/tracker_test.go
package activity

import (
	"testing"
	"time"
)

func TestTrackerHooksAndScreenFallback(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tr := NewTracker(3*time.Second, func() time.Time { return now })

	// Hook: working -> blocked -> viewed clears to idle.
	if s := tr.OnHook("feat-a", "working", false); s != StateWorking {
		t.Fatalf("expected working, got %s", s)
	}
	if s := tr.OnHook("feat-a", "blocked", false); s != StateBlocked {
		t.Fatalf("expected blocked, got %s", s)
	}
	if s := tr.MarkViewed("feat-a"); s != StateIdle {
		t.Fatalf("expected MarkViewed to clear blocked to idle, got %s", s)
	}

	// Screen-change fallback on feat-b (no hooks): initial poll establishes baseline (Idle).
	if s := tr.OnPoll("feat-b", "prompt >", false, false, false); s != StateIdle {
		t.Fatalf("expected initial poll idle, got %s", s)
	}
	// Output changes -> Working.
	now = now.Add(1 * time.Second)
	if s := tr.OnPoll("feat-b", "thinking...", false, false, false); s != StateWorking {
		t.Fatalf("expected working on text change, got %s", s)
	}
	// Quiet for >3s while not viewed -> Done (unread).
	now = now.Add(4 * time.Second)
	if s := tr.OnPoll("feat-b", "thinking...", false, false, false); s != StateDone {
		t.Fatalf("expected done after timeout while unviewed, got %s", s)
	}
}
```

```go
// internal/hooks/hooks_test.go
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudeHooksPreservesExistingSettingsAndExcludes(t *testing.T) {
	wt := t.TempDir()
	common := t.TempDir()
	_ = os.MkdirAll(filepath.Join(wt, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(wt, ".claude", "settings.local.json"), []byte(`{"env":{"FOO":"bar"}}`), 0o644)

	if err := InstallWorktreeHooks("claude", wt, common, "/usr/local/bin/agenttree"); err != nil {
		t.Fatalf("InstallWorktreeHooks: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(wt, ".claude", "settings.local.json"))
	if !strings.Contains(string(data), `"FOO"`) || !strings.Contains(string(data), `"UserPromptSubmit"`) {
		t.Fatalf("expected merged settings.local.json, got: %s", data)
	}
	excl, _ := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if !strings.Contains(string(excl), ".claude/settings.local.json") {
		t.Fatalf("expected .claude/settings.local.json in info/exclude, got: %s", excl)
	}
}

func TestWrapCodexCommand(t *testing.T) {
	got := WrapAgentCommand("codex", "codex", "agenttree")
	if !strings.Contains(got, `notify=["agenttree","hook","done"]`) {
		t.Fatalf("unexpected wrapped codex cmd: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/activity ./internal/hooks -v`
Expected: FAIL

- [ ] **Step 3: Implement `internal/activity/tracker.go` and `internal/hooks/hooks.go`**

- In `internal/activity/tracker.go`: track per-feature `lastHash`, `lastChangeAt`, `state`, and `hookActive`. Normalize `screenText` by trimming trailing spaces per line before `sha256` hashing so cursor blink doesn't count. Follow the §6.4 state table (`Working`, `Blocked`, `Done`, `Idle`, `Stopped`).
- In `internal/hooks/hooks.go`:
  - For `agentID == "claude"`, unmarshal `<worktreePath>/.claude/settings.local.json` into `map[string]any` (creating `.claude` dir if needed), merge hook entries for `UserPromptSubmit` (`"<bin> hook working"`), `PreToolUse` (`"<bin> hook working"`), `Notification` (`"<bin> hook blocked"`), and `Stop` (`"<bin> hook done"`), write back formatted JSON, and ensure `.claude/settings.local.json` is listed in `<gitCommonDir>/info/exclude`.
  - In `WrapAgentCommand`: for `codex`, inject `-c 'notify=["<bin>","hook","done"]'` after the `codex` binary token; for other agents, return `baseCmd` unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/activity ./internal/hooks -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/activity/ internal/hooks/
git commit -m "feat(activity,hooks): add status state machine and Claude/Codex hook wiring"
```

---

### Task 6 (PR 6): Unix Control Socket (`internal/ctl`) & GitHub PR Poller (`internal/pr`)

**Files:**
- Create: `internal/ctl/ctl.go`
- Create: `internal/pr/pr.go`
- Test: `internal/ctl/ctl_test.go`
- Test: `internal/pr/pr_test.go`

**Interfaces:**
- Consumes: none
- Produces (`package ctl` & `package pr`):
  ```go
  // package ctl
  type Message struct {
      Kind    string `json:"kind"`    // "action" | "hook"
      Action  string `json:"action"`  // "new", "next", "prev", "jump:1".."jump:9", "lazygit", "cleanup", "restart", "settings"
      Feature string `json:"feature"` // for Kind == "hook"
      Event   string `json:"event"`   // "working" | "blocked" | "done"
  }
  type Server struct { /* listener + msg chan */ }
  func SocketPath(gitCommonDir string) string
  func StartServer(sockPath string) (*Server, error)
  func (s *Server) Messages() <-chan Message
  func (s *Server) Close() error
  func Send(sockPath string, msg Message) error

  // package pr
  type Status struct {
      Number      int
      Branch      string
      Display     string // "draft" | "open" | "approved" | "changes requested" | "merged" | "closed"
      CIMark      string // "✓" | "✗" | "…" | ""
      URL         string
      IsMerged    bool
  }
  func ParseGHOutput(jsonBytes []byte) (map[string]Status, error) // branch -> Status
  func FetchAll(repoRoot string) (map[string]Status, bool, error)  // bool = ghAvailable
  ```

- [ ] **Step 1: Write failing tests for `ctl` socket roundtrip and `pr` JSON parsing**

Create `internal/ctl/ctl_test.go` and `internal/pr/pr_test.go`:

```go
// internal/ctl/ctl_test.go
package ctl

import (
	"path/filepath"
	"testing"
	"time"
)

func TestServerClientRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	srv, err := StartServer(sock)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer srv.Close()

	want := Message{Kind: "hook", Feature: "auth", Event: "blocked"}
	if err := Send(sock, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-srv.Messages():
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}
```

```go
// internal/pr/pr_test.go
package pr

import "testing"

func TestParseGHOutput(t *testing.T) {
	sample := []byte(`[
		{"number":42,"headRefName":"feat-auth","state":"OPEN","isDraft":false,"reviewDecision":"APPROVED","statusCheckRollup":[{"conclusion":"SUCCESS"}],"url":"https://github.com/x/y/pull/42"},
		{"number":43,"headRefName":"feat-old","state":"MERGED","isDraft":false,"reviewDecision":"","statusCheckRollup":[],"url":"https://github.com/x/y/pull/43"}
	]`)
	byBranch, err := ParseGHOutput(sample)
	if err != nil {
		t.Fatalf("ParseGHOutput: %v", err)
	}
	if byBranch["feat-auth"].Display != "approved" || byBranch["feat-auth"].CIMark != "✓" {
		t.Fatalf("unexpected feat-auth status: %+v", byBranch["feat-auth"])
	}
	if !byBranch["feat-old"].IsMerged || byBranch["feat-old"].Display != "merged" {
		t.Fatalf("unexpected feat-old status: %+v", byBranch["feat-old"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ctl ./internal/pr -v`
Expected: FAIL

- [ ] **Step 3: Implement `internal/ctl/ctl.go` and `internal/pr/pr.go`**

- In `internal/ctl/ctl.go`: remove any stale socket file before `net.Listen("unix", sockPath)`, accept connections in a goroutine, decode one JSON `Message` per connection, and push onto a buffered channel (`make(chan Message, 32)`).
- In `internal/pr/pr.go`: implement `ParseGHOutput` mapping `state` (`MERGED` -> `"merged"`, `CLOSED` -> `"closed"`, `OPEN` + `isDraft` -> `"draft"`, `OPEN` + `reviewDecision == "APPROVED"` -> `"approved"`, `OPEN` + `reviewDecision == "CHANGES_REQUESTED"` -> `"changes requested"`, else `"open"`) and `statusCheckRollup` (`FAILURE`/`ERROR` -> `"✗"`, `PENDING`/`IN_PROGRESS` -> `"…"`, all `SUCCESS` -> `"✓"`). `FetchAll` checks `exec.LookPath("gh")` and runs `gh pr list --state all --json number,headRefName,state,isDraft,reviewDecision,statusCheckRollup,url --limit 100` in `repoRoot`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ctl ./internal/pr -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ctl/ internal/pr/
git commit -m "feat(ctl,pr): add Unix control socket and GitHub PR status poller"
```

---

### Task 7 (PR 7): Worktree Setup Detection, Copy & Install (`internal/setup`)

**Files:**
- Create: `internal/setup/setup.go`
- Test: `internal/setup/setup_test.go`

**Interfaces:**
- Consumes: `config.SetupConfig` from `internal/config`
- Produces (`package setup`):
  ```go
  type DepInfo struct {
      Lockfile   string // e.g. "pnpm-lock.yaml"
      CopyDir    string // e.g. "node_modules" (empty if no local dir like go.mod)
      InstallCmd string // e.g. "pnpm install"
  }
  func DetectCopyCandidates(repoRoot string) ([]string, error)
  func DetectDeps(repoRoot string) DepInfo
  func Run(repoRoot, worktreePath string, cfg config.SetupConfig, out io.Writer) error
  ```

- [ ] **Step 1: Write failing tests for candidate detection, lockfile mapping, and non-overwriting file copy**

Create `internal/setup/setup_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/setup -v`
Expected: FAIL

- [ ] **Step 3: Implement `internal/setup/setup.go`**

Implement `DetectCopyCandidates`, `DetectDeps` (matching the exact lockfile table in §6.11: `package-lock.json` → `npm ci` / `node_modules`, `pnpm-lock.yaml` → `pnpm install` / `node_modules`, `yarn.lock` → `yarn install` / `node_modules`, `bun.lock*` → `bun install` / `node_modules`, `composer.json` → `composer install` / `vendor`, `Gemfile.lock` → `bundle install` / `vendor`, `uv.lock` → `uv sync` / `.venv`, `go.mod` → `go mod download` / `""`), and `Run`:
- For each pattern in `cfg.Copy`, resolve matches in `repoRoot`; if `<worktreePath>/<rel>` already exists (`os.Stat` succeeds), skip it; otherwise copy preserving permissions.
- If `cfg.Deps == "copy"`: for any existing directory among `node_modules`, `vendor`, `.venv` in `repoRoot`, run `cp -cR <src> <dst>` on `runtime.GOOS == "darwin"` (falling back to `cp -R` if CoW fails or on non-darwin).
- If `cfg.Deps == "install"` and `cfg.InstallCmd != ""`: run `sh -c <cfg.InstallCmd>` with `cmd.Dir = worktreePath` and `Stdout`/`Stderr` wired to `out`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/setup -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/setup/
git commit -m "feat(setup): add worktree file detection, CoW copy, and dependency install"
```

---

### Task 8 (PR 8): Bubble Tea Main UI (`internal/ui`: Sidebar, Tabs, Switching, Lazygit Popup, Restart/Reconcile)

**Files:**
- Create: `internal/ui/model.go`
- Create: `internal/ui/view.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `internal/config`, `internal/state`, `internal/gitwt`, `internal/mux`, `internal/activity`, `internal/hooks`, `internal/ctl`, `internal/pr`
- Produces (`package ui`):
  ```go
  type AppDeps struct {
      RepoRoot     string
      GitCommonDir string
      SelfPane     mux.PaneID
      SlotPane     mux.PaneID
      WindowID     string
      BgSession    string
      SockPath     string
      BinPath      string
      Mux          mux.Multiplexer
  }
  type Model struct { /* Bubble Tea model */ }
  func NewModel(deps AppDeps, cfg config.Config, st *state.State) *Model
  func (m *Model) ReconcileOnStart() error
  func (m *Model) Init() tea.Cmd
  func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
  func (m *Model) View() string
  ```

- [ ] **Step 1: Initialize Bubble Tea & Lip Gloss dependencies and write failing UI model tests**

Run `go get github.com/charmbracelet/bubbletea@latest github.com/charmbracelet/lipgloss@latest`. Create `internal/ui/model_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gcg/agenttree/internal/config"
	"github.com/gcg/agenttree/internal/ctl"
	"github.com/gcg/agenttree/internal/mux"
	"github.com/gcg/agenttree/internal/state"
)

type fakeMux struct {
	mux.Multiplexer
	shownFeature mux.PaneID
	popups       []string
	panes        map[string]mux.PaneID
}

func (f *fakeMux) FindPanes(session string) (map[string]mux.PaneID, error) { return f.panes, nil }
func (f *fakeMux) Show(feat, slot mux.PaneID) error                        { f.shownFeature = feat; return nil }
func (f *fakeMux) Popup(cwd, cmd string, w, h int) error                   { f.popups = append(f.popups, cwd+":"+cmd); return nil }

func TestModelSwitchAndLazygitPopup(t *testing.T) {
	fm := &fakeMux{panes: map[string]mux.PaneID{"feat-1": "%11", "feat-2": "%12"}}
	cfg := config.Default()
	st := &state.State{
		Features: []state.Feature{
			{Name: "feat-1", Branch: "feat-1", WorktreePath: "/wt/feat-1", AgentID: "claude", CreatedAt: time.Now()},
			{Name: "feat-2", Branch: "feat-2", WorktreePath: "/wt/feat-2", AgentID: "claude", CreatedAt: time.Now()},
		},
		SelectedFeature: "feat-1",
	}
	m := NewModel(AppDeps{RepoRoot: "/repo", GitCommonDir: t.TempDir(), SlotPane: "%2", BgSession: "_bg", Mux: fm}, cfg, st)
	_ = m.ReconcileOnStart()

	// Next action switches to feat-2 and swaps pane %12 into slot %2.
	updated, _ := m.Update(ctl.Message{Kind: "action", Action: "next"})
	m = updated.(*Model)
	if m.SelectedFeature() != "feat-2" || fm.shownFeature != "%12" {
		t.Fatalf("expected feat-2 (%12), got selected=%q pane=%q", m.SelectedFeature(), fm.shownFeature)
	}

	// Lazygit action opens popup in /wt/feat-2.
	updated, _ = m.Update(ctl.Message{Kind: "action", Action: "lazygit"})
	m = updated.(*Model)
	if len(fm.popups) != 1 || !strings.HasPrefix(fm.popups[0], "/wt/feat-2:lazygit") {
		t.Fatalf("expected lazygit popup in /wt/feat-2, got %v", fm.popups)
	}

	// View renders both features in sidebar mode.
	view := m.View()
	if !strings.Contains(view, "feat-1") || !strings.Contains(view, "feat-2") {
		t.Fatalf("expected view to contain feature names, got:\n%s", view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -v`
Expected: FAIL (`undefined: NewModel`)

- [ ] **Step 3: Implement `internal/ui/model.go` and `internal/ui/view.go`**

Implement `Model` and `View`:
- `ReconcileOnStart`:
  - Discover manual worktrees via `gitwt.ListWorktrees` that aren't in `state.json` and add them with `StateStopped`.
  - Discover live panes via `Mux.FindPanes(BgSession)`.
  - For each feature in `state.json` whose pane is missing (e.g. after tmux/machine restart), spawn a new background pane running `"sh -c '<resumeCmd> || <launchCmd>'"` (wrapped with `hooks.WrapAgentCommand` and with `AGENTTREE_FEATURE` + `AGENTTREE_CTL` env vars), and swap the selected feature into `SlotPane`.
- `Update`:
  - Handle `tea.KeyMsg` (`c`, `j`/`down`, `k`/`up`, `enter`, `g`, `x`, `r`, `s`, `q`, `Q`), `tea.MouseMsg` (click on tab row/column or `[+]`), and `ctl.Message` (`new`, `next`, `prev`, `jump:1`..`jump:9`, `lazygit`, `cleanup`, `restart`, `settings`, plus `Kind == "hook"` updating `activity.Tracker`).
  - On `lazygit`: call `Mux.Popup(activeFeature.WorktreePath, "lazygit", 90, 90)`.
  - Periodic 1s tick polls `Mux.Capture`, `Mux.IsDead`, `Mux.HasBell` and feeds `activity.Tracker.OnPoll`; periodic `cfg.PR.PollIntervalSec` tick calls `pr.FetchAll`.
  - When a popup (`new`, `settings`, `cleanup`) completes and returns, reload `config.Load` and `state.Load` and reconcile any newly added or removed feature.
- `View`:
  - Render `sidebar` (vertical list of features + `[+ New]`) or `tabs` (horizontal tab bar + `[+]`), showing status symbol (`⠋`/`🔔`/`✓`/`💤`/`⏹`), feature name, and PR badge (`draft`/`open`/`approved`/`changes requested`/`merged [Clean up?]`/`closed` + CI `✓`/`✗`/`…`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/ui/
git commit -m "feat(ui): add Bubble Tea sidebar/tabs UI, pane switching, and lazygit popup"
```

---

### Task 9 (PR 9): Huh Popups & CLI Entrypoint (`internal/ui/popups.go`, `cmd/agenttree/main.go`)

**Files:**
- Create: `internal/ui/popups.go`
- Create: `cmd/agenttree/main.go`
- Test: `internal/ui/popups_test.go`

**Interfaces:**
- Consumes: `internal/config`, `internal/state`, `internal/gitwt`, `internal/setup`, `internal/hooks`, `internal/mux`, `internal/ctl`, `internal/pr`, `internal/ui`
- Produces:
  - `package ui`:
    ```go
    func RunOnboardingPopup(repoRoot, gitCommonDir string) error
    func RunFirstSetupPopup(repoRoot, gitCommonDir string) error
    func RunNewFeaturePopup(repoRoot, gitCommonDir, bgSession, binPath, sockPath string, m mux.Multiplexer) error
    func RunCleanupPopup(repoRoot, gitCommonDir, featureName string, prMerged bool, m mux.Multiplexer, bgSession string) error
    func RunSettingsPopup(repoRoot, gitCommonDir string) error
    ```
  - `cmd/agenttree/main.go`: CLI subcommands `(none)`, `ctl`, `hook`, `setup`, `new`, `settings`, `cleanup`, `placeholder`.

- [ ] **Step 1: Add `charmbracelet/huh` dependency and write failing popup validation & CLI tests**

Run `go get github.com/charmbracelet/huh@latest`. Create `internal/ui/popups_test.go`:

```go
package ui

import (
	"testing"

	"github.com/gcg/agenttree/internal/state"
)

func TestValidateNewFeatureName(t *testing.T) {
	existing := []state.Feature{{Name: "auth-flow", Branch: "auth-flow"}}
	if err := ValidateFeatureName("", "", existing); err == nil {
		t.Fatal("expected empty name to fail")
	}
	if err := ValidateFeatureName("auth-flow", "", existing); err == nil {
		t.Fatal("expected duplicate feature name to fail")
	}
	if err := ValidateFeatureName("payments api", "feat/", existing); err != nil {
		t.Fatalf("expected valid name to pass, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -v`
Expected: FAIL (`undefined: ValidateFeatureName`)

- [ ] **Step 3: Implement `internal/ui/popups.go` and `cmd/agenttree/main.go`**

- In `internal/ui/popups.go`:
  - `ValidateFeatureName(name, prefix string, existing []state.Feature) error`: checks non-empty, checks `gitwt.Slugify`, and checks uniqueness against `existing`.
  - `RunOnboardingPopup`: Huh form asking for `WorktreeDir` (default `.worktrees`) and `IgnoreMode` (`gitignore-commit`, `gitignore`, `exclude`), runs `gitwt.EnsureIgnored`, and saves per-repo `config.toml`.
  - `RunFirstSetupPopup`: detects candidates via `setup.DetectCopyCandidates` and `setup.DetectDeps`, presents Huh multi-select for files to copy and select for deps (`copy`, `install`, `none`) + editable install command, sets `Setup.Configured = true`, and saves per-repo `config.toml`.
  - `RunNewFeaturePopup`: if `!cfg.Setup.Configured`, runs `RunFirstSetupPopup` first. Presents Huh form for **Name** and **Agent** (default `cfg.DefaultAgent`). Creates worktree (`gitwt.AddWorktree`), installs agent hooks (`hooks.InstallWorktreeHooks`), records feature in `state.json` (and sets `SelectedFeature`), and creates the background tmux window running `"<binPath> setup --feature <slug> && exec <wrappedAgentCmd>"`.
  - `RunCleanupPopup`: runs `gitwt.CheckSafety`; shows Huh confirmation listing steps (`kill pane`, `git worktree remove`, `git branch -d`, remove tab). If `safety.NeedsForce()` is true, displays the exact warning (uncommitted changes or unmerged/unpushed commits) and requires a second explicit "Force delete" confirmation before calling `gitwt.RemoveWorktree(..., true)`.
  - `RunSettingsPopup`: Huh form with two groups/tabs (**Global** and **This Repo**) editing and saving `GlobalConfigPath()` and `RepoConfigPath(gitCommonDir)`.
- In `cmd/agenttree/main.go`:
  - Dispatch on `os.Args[1]`:
    - `(none)`: verify `mux.InTmux()`, resolve repo via `gitwt.ResolveRepo(".")`, acquire lock (`state.AcquireLock`; if `LockHeldError`, print message and offer to select that pane), run onboarding if `!repoConfigExists`, start `ctl.StartServer`, split slot via `SetupWindowSlot`, bind tmux keys, run `tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())`, and call `CleanupWindow` on exit.
    - `ctl <action> [--window <id>]`: read `@agenttree_ctl` from window via `tmux show-options -w -v -t <id> @agenttree_ctl`; if empty, `tmux display-message "no agenttree in this window"`; otherwise `ctl.Send(sock, ctl.Message{Kind: "action", Action: action})`.
    - `hook <event>`: read `AGENTTREE_CTL` and `AGENTTREE_FEATURE` from env and call `ctl.Send`.
    - `setup --feature <name>`: load config & state, run `setup.Run(repoRoot, feature.WorktreePath, cfg.Setup, os.Stdout)`.
    - `new`, `settings`, `cleanup`: invoke the corresponding popup function in `internal/ui`.
    - `placeholder`: print centered `"Press prefix + C (or c) to start a feature"` and block until killed.

- [ ] **Step 4: Run all tests and verify full binary build**

Run: `go test ./... -v && go build ./cmd/agenttree`
Expected: PASS and `agenttree` binary compiled with zero errors.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/popups.go internal/ui/popups_test.go cmd/agenttree/main.go
git commit -m "feat(cli,ui): add Huh popups and agenttree CLI subcommands"
```
