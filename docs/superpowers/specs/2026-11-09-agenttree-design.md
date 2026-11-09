# agenttree: Design

Status: **Draft v4 (tmux-backed, market research complete, lazygit popup added)**
Date: 2026-11-09

## 1. Intent

The owner currently works on one feature at a time: branch, PR, wait, repeat.
Git worktrees would let several features progress in parallel, each with its own
coding agent, but worktrees feel scary and fiddly, so they go unused.

**agenttree** is a Go terminal app that makes "one feature = one worktree = one
agent" the easy default. The owner already works in tmux with vim in one pane and
`claude` / `agy` in another. agenttree takes over the agent side: it shows a list
of features (tabs or sidebar) and, next to it, the live agent for the selected
feature. Switching tabs swaps which agent is shown.

Success looks like: the owner routinely has 2–4 features in flight, never types a
`git worktree` command, and can tell at a glance which agent needs them and which
PRs are ready or merged.

## 2. Goals

1. List every active feature (worktree) for the current repo as a **tab bar** or a
   **sidebar**, configurable in settings.
2. Create a new feature from a keybinding or a "+" tab: ask for a name (and agent),
   create the worktree and branch, launch the agent inside it.
3. Show the selected feature's live agent next to the list; typing goes straight
   to the agent, exactly as in a normal tmux pane today.
4. Supported agents: Claude Code, Antigravity (`agy`), Codex, OpenCode. Default
   agent is configurable. Each agent's launch/resume command is configurable.
5. Each tab shows: feature name, agent activity state, PR existence and status.
6. When a PR is merged, offer a one-confirmation **Clean up** (stop agent, remove
   worktree, delete local branch, close tab).
7. Agents keep running when agenttree quits; reopening agenttree reconnects to
   them. After a tmux/machine restart, agenttree relaunches each feature with the
   agent it was using (resuming the previous conversation when possible).
8. Open `lazygit` for the selected feature's worktree in a floating tmux popup
   (like `lazygit.nvim`) with a single keybinding, closing cleanly on `q`.
9. In-app settings screen (Charm Huh forms).

## 3. Non-goals

- Building our own code/diff viewer, editor integration, or opening vim (diffing
  and git staging are delegated to the optional `lazygit` popup).
- Creating, reviewing, or merging PRs inside agenttree. agenttree only *reads*
  PR status.
- Managing multiple repositories in one agenttree instance (run one per repo).
- Our own terminal emulation. tmux draws the agents.
- Working without a multiplexer in v1. tmux is required.
- Windows support (macOS first, Linux expected to work).

## 4. Anti-goals (would make it a failure even if it "works")

- Losing work: removing a worktree or branch that has uncommitted or unpushed
  changes without a clear, explicit warning.
- Wrecking the owner's tmux setup: stealing their keybindings, leaving stray
  panes/sessions/bindings behind, or rearranging their windows unexpectedly.
- Making worktrees *more* confusing: surprising paths, stray folders, or git
  state the owner has to fix by hand.
- A bloated UI. Claude Squad (§11) does the right job with too much UI; agenttree
  should feel minimal.

## 5. Decided by the owner

| Topic | Decision |
|---|---|
| Language / UI | Go, Charm stack (Bubble Tea, Lip Gloss, Huh for settings/prompts) |
| Agent hosting | **tmux** owns the agent processes and screens; agenttree is the controller/UI |
| Future backends | Support Ghostty's creator's upcoming multiplexer later (see §6.12) |
| Worktree location | `<repo>/.worktrees/<feature>` by default, configurable in settings |
| Ignoring worktrees | Default: add to `.gitignore` **and commit it**. Asked per repo during onboarding; editable in settings (§6.10) |
| Worktree setup | Asked once, on the first new feature in a repo: which files to copy (`.env` …) and how to get dependencies (copy `node_modules/`/`vendor/` or run install). That answer becomes the repo default, applied automatically after; editable in settings (§6.11) |
| Status signals | Use agent hooks where available in v1; screen-change guess as fallback (§6.4) |
| Config format | TOML |
| tmux server | Use the owner's existing tmux server; never start a separate one (§6.1) |
| Layout | Tab bar or sidebar, configurable |
| Agents | Claude Code, Antigravity, Codex, OpenCode; default configurable; pick per feature |
| Cleanup | Only after confirmation; offered when PR is merged |
| Restart | Auto-relaunch each feature with its agent; resume session if possible |
| Status | Distinguish working / blocked (needs input) / done (unread) / idle (inspired by Herdr) |
| Scope | Worktrees + running agents + `lazygit` popup; no built-in code/diff UI |
| Git / diff inspection | `lazygit` in a floating tmux popup (`lazygit.nvim` style) for the active worktree |
| Build vs adopt | Build our own inside the owner's existing tmux setup; Claude Squad is bloated/unmaintained, and Herdr replaces tmux with its own multiplexer |

## 6. Design (builder's calls unless stated)

### 6.1 tmux layout

- agenttree must be started inside tmux (`$TMUX` set); otherwise print a clear
  message and exit.
- On start, agenttree **splits its own pane** to create the **agent slot**:
  - `sidebar` layout: agenttree is a narrow pane (default 24 cols) on the left,
    agent slot on the right.
  - `tabs` layout: agenttree is a short pane (default 2 rows) on top, agent slot
    below.
- **One tmux server, the owner's.** agenttree talks to the server it is
  running in (from `$TMUX`) and never starts its own. `swap-pane` only works
  within a server, and a separate server would mean nested tmux.
- Agents live in a detached **background session per repo** (not per
  agenttree instance), named `_agenttree-<repo-name>-<4-char-hash-of-root>`,
  one window per feature. The `_agenttree-` prefix keeps these sorted together
  and easy to filter out of session pickers; the owner's own sessions are never
  touched. Several agenttree instances for different repos (in different
  windows or sessions) each get their own background session. A second
  instance for the same repo is refused (§6.7 lock) and offers to jump to the
  first one.
- **Switching** = `tmux swap-pane` between the agent slot and the target
  feature's pane in the background session, then focus the slot. Panes are
  tracked by pane id (`%N`) and tagged with tmux user options
  (`@agenttree_feature=<name>`, `@agenttree_repo=<root>`) so agenttree can
  rediscover them after it restarts.
- **Empty state** (no features or none selected): the slot runs a tiny
  placeholder (`agenttree placeholder`: "Press <key> to start a feature").
- **On quit**: swap the visible agent back into the background session, kill
  the slot and the agenttree pane. Agents keep running.

### 6.2 Keybindings (the tmux part)

While you type in the agent slot, agenttree's own pane doesn't receive keys. So
global actions are **tmux key bindings**. tmux bindings apply to the whole
server, so they're shared by all agenttree instances:

- Each binding runs `agenttree ctl <action> --window #{window_id}`.
- Each running instance tags its tmux window with
  `@agenttree_ctl=<socket path>`; the socket lives at
  `<git-common-dir>/agenttree/ctl.sock`. `ctl` reads that tag from the window
  the key was pressed in and sends the action to that instance. If the window
  has no agenttree, it shows `display-message "no agenttree in this window"`.
- The first instance installs the bindings; the last one to quit removes them
  (it checks for other windows still tagged `@agenttree_ctl`).

Defaults (all configurable; chosen to avoid clobbering common tmux setups):
- `prefix` + `C` new feature · `prefix` + `)`/`(` next/prev ·
  `M-1`…`M-9` jump to feature N (root table) · `prefix` + `G` open `lazygit`
  popup for active feature · `prefix` + `X` clean up · `prefix` + `R` restart
  agent · `prefix` + `S` settings (not `,`, which is tmux's rename-window).
- Before binding, agenttree checks for an existing binding on that key and,
  if one exists, skips it and shows a warning in settings instead of
  overwriting it.
- When agenttree's pane has focus, plain keys work too (`c`, `j`/`k`, `enter`,
  `g`, `x`, `s`, `q`), and mouse clicks on tabs or "+" work (tmux `mouse on`).
- Prompts and dialogs (new feature, settings, confirm) open as a **tmux popup**
  (`display-popup -E agenttree <subcommand>`) so they appear in front no matter
  which pane has focus. *(Requires tmux ≥ 3.2.)*
- **`lazygit` popup (`prefix` + `G` / `g`):** runs
  `tmux display-popup -E -w 90% -h 90% -d <worktree-path> lazygit`, floating
  over the window just like `lazygit.nvim` and closing automatically when
  `lazygit` exits. If `lazygit` is not installed (or no feature is selected),
  shows a brief `display-message` hint.

### 6.3 Repo & worktree model

- Start from anywhere in a git repo, including inside one of its worktrees; the
  main repo root is resolved via `git rev-parse --git-common-dir`.
- A **feature** = worktree under the configured folder + branch + recorded agent.
  The main checkout is **not** a tab.
- Feature name → branch: slugify (lowercase; spaces/invalid → `-`), plus an
  optional configurable `branch_prefix` (default empty).
- Base for new branches: the repo's default branch (`origin/HEAD`) after
  `git fetch origin <default>`; fall back to the local default branch, then
  `HEAD`, if offline.
- Existing branch with that name → check it out into the worktree instead.
  Existing feature with that name → just switch to it.
- Worktrees found under the folder but not in state (made by hand) appear as
  tabs with a stopped agent.

### 6.4 Activity status

Two signal sources, merged per feature. **Hooks win** when the agent has them;
the screen-change guess covers agents without hooks and fills gaps.

**A. Agent hooks (v1 for agents that support them).** Each hook runs
`agenttree hook <event>`; the agent pane's env carries
`AGENTTREE_FEATURE` and `AGENTTREE_CTL` (socket path), so the event reaches
the right instance and feature. Events map to: `working` (prompt submitted /
tool running), `blocked` (permission prompt / question mid-turn), `done` (turn
finished). *(Note: Herdr's open-source agent integrations are a good reference
for exact hook wiring across Claude Code, Antigravity, Codex, and OpenCode.)*

| Agent | Mechanism (verify during build) | Installed how |
|---|---|---|
| Claude Code | hooks `UserPromptSubmit`, `PreToolUse`, `Notification`, `Stop` | written to the worktree's `.claude/settings.local.json`, merged with any existing content; agenttree makes sure that file is ignored via `.git/info/exclude` so it never shows up as a change |
| Codex | `notify` program (turn complete) | passed at launch: `codex -c notify=[...]`, so the owner's global config isn't edited |
| Antigravity | hooks (see antigravity.google/docs/hooks) | per-worktree config if supported; otherwise fallback only |
| OpenCode | plugin/event system | later; fallback only in v1 |

**B. Screen-change guess (fallback).** Poll every 1s with
`tmux capture-pane -p -t <pane>` for each feature pane and hash the visible
text. A change means activity; cursor blink doesn't count.

| State | Rule | Display |
|---|---|---|
| Working | hook said `working`, or content changed within `status.working_timeout` (default 3s) | spinner |
| Blocked | hook said `blocked` (waiting on permission/question mid-turn), or window bell while not viewed | 🔔 highlighted |
| Done | hook said `done` (turn finished), or (no hooks) was Working then quiet; and not viewed since | ✓ highlighted |
| Idle | quiet and already viewed | 💤 |
| Stopped | pane dead / process exited | ⏹ |

Showing a feature in the slot clears "Blocked" / "Done" to "Idle".

### 6.5 PR status

- `gh pr list --state all --json number,headRefName,state,isDraft,reviewDecision,statusCheckRollup,url --limit 100`
  once per poll, matched to features by branch name.
- Poll every 60s, and soon after a feature is selected (debounced). If `gh` is
  missing or not logged in: no PR info, plus a one-time hint.
- Shown as: none · draft · open · approved · changes requested · merged · closed,
  plus a CI ✓/✗/… mark. Merged → "Clean up?" on that tab.

### 6.6 Cleanup

From the merged affordance or manually. A confirmation popup lists the steps:
stop agent (kill pane), `git worktree remove`, `git branch -d`, remove the tab.
If the worktree has uncommitted changes, or the branch isn't merged or pushed,
the dialog says exactly that and requires a second explicit "force" confirm
(`--force` / `-D`). Squash merges look "unmerged" to git, so a merged PR on
GitHub counts as merged. Remote branches are not touched.

### 6.7 Persistence & restart

- State file: `<git-common-dir>/agenttree/state.json` (never committed). Per
  feature: name, branch, worktree path, agent id, created-at. Plus the last
  selected feature. A lock file prevents two agenttree instances per repo
  (the second one says "already running in pane X" and offers to focus it).
- Startup: load state, reconcile with `git worktree list` and with panes in the
  background session (matched by `@agenttree_feature`).
  - Pane alive → reuse it (agents survived).
  - Pane missing (tmux/machine restarted) → open a new window in the background
    session and run the agent's **resume** command; if it exits non-zero within
    ~3s, run **launch** instead.
- Default commands (each verified during implementation):

| Agent | Launch | Resume |
|---|---|---|
| Claude Code | `claude` | `claude --continue` |
| Antigravity | `agy` | `agy --continue` (per-directory scoping unverified) |
| Codex | `codex` | `codex resume --last` (scoped to cwd) |
| OpenCode | `opencode` | `opencode --continue` |

- Panes are created with `remain-on-exit on`, so a crashed agent shows its exit
  output and the tab shows Stopped; "restart agent" respawns it
  (`respawn-pane`).
- `q` in agenttree leaves agents running. A separate "Stop all agents and quit"
  action (with confirmation) kills the background session.
- Later: capture exact session IDs (`claude --resume <id>`, `agy --conversation <id>`).

### 6.8 Settings

- **TOML.** Global: `~/.config/agenttree/config.toml`. Per-repo:
  `<git-common-dir>/agenttree/config.toml` (local to the machine, never
  committed). Per-repo values override global ones.
- Settings popup has two tabs: **Global** and **This repo**.
- Global keys: `layout` (`tabs`|`sidebar`), `sidebar_width`, `default_agent`,
  `branch_prefix`, `agents.<id>.launch` / `.resume`, `keys.*`,
  `status.working_timeout`, `pr.poll_interval`.
- Repo keys (filled by onboarding, editable): `worktree_dir`
  (default `.worktrees`), `ignore_mode` (§6.10), `setup.copy` (list of
  paths/globs), `setup.deps` (`none`|`copy`|`install`), `setup.install_cmd`,
  plus any global key as an override.

### 6.9 New-feature popup

Huh form: **Name** (required; unique; slug must be a valid git ref) and **Agent**
(select, preselected to `default_agent`). On submit: create worktree (inline
progress/errors), add tab, start the feature's window running setup (§6.11)
then the agent, swap it into the slot, focus it. On the repo's first feature,
the setup popup (§6.11) appears before the worktree is created.

### 6.10 Repo onboarding (first run in a repo)

Shown as a popup the first time agenttree runs in a repo (no per-repo config
yet):

1. **Worktree folder:** default `.worktrees`.
2. **How to ignore it:**
   - `gitignore-commit` (**default**): append `/.worktrees/` to `.gitignore`
     and commit only that file:
     `git commit --only .gitignore -m "chore: ignore .worktrees (agenttree)"`.
     `--only` commits just that file even if other changes are staged. It
     commits on whatever branch the main checkout is on and does **not** push.
     If the main checkout is in the middle of a merge/rebase, fall back to
     `gitignore` and say why.
   - `gitignore`: append to `.gitignore`, don't commit.
   - `exclude`: write to `.git/info/exclude` (local only, nothing to commit).
   - Skip if the folder is already ignored (`git check-ignore`).
3. Answers saved to the per-repo config; editable later in settings. Changing
   `worktree_dir` later doesn't move existing worktrees (`git worktree move`
   is a later feature).

### 6.11 Worktree setup (first feature in a repo, then automatic)

A fresh worktree has none of the main checkout's untracked or ignored files.

- **First new feature in a repo** → a popup *before* the worktree is created:
  - **Files to copy:** checkboxes pre-filled from what's found in the main
    checkout, ignored or untracked: `.env`, `.env.*`, `.envrc`,
    `.tool-versions`, `*.local.*` config files. Preselected: `.env*`.
  - **Dependencies:** detected from lockfiles; choose one:
    - *Copy* existing dirs (`node_modules/`, `vendor/`, `.venv/`), using
      copy-on-write clones on macOS (`cp -c`) so it's fast and takes little disk,
    - *Install* with the detected command (`package-lock.json` → `npm ci`,
      `pnpm-lock.yaml` → `pnpm install`, `yarn.lock` → `yarn install`,
      `bun.lock*` → `bun install`, `composer.json` → `composer install`,
      `Gemfile.lock` → `bundle install`, `uv.lock` → `uv sync`,
      `go.mod` → `go mod download`); command editable,
    - *Nothing*.
  - The choice is saved as the repo default (`setup.*`) and applied
    automatically to every later feature. No more popups; editable in settings.
- **Where it runs:** in the feature's own tmux window, before the agent:
  the pane runs `agenttree setup --feature <name> && exec <agent cmd>`. That
  way copy/install output is visible in the slot, the UI never blocks, and the
  tab shows a "setting up" state. If setup fails, the pane stays open showing
  the error, with "restart agent" to retry, or skip setup and launch anyway.
- Copying files never overwrites a file that already exists in the worktree.

### 6.12 Multiplexer abstraction

All tmux calls go through one Go interface so another backend (Ghostty
creator Mitchell Hashimoto's new multiplexer from Superlogical, reportedly
called "Rex"; details unconfirmed) can be added later without touching the UI:

```go
type Multiplexer interface {
    EnsureSession(name string) error
    NewAgentPane(session, feature, cwd, cmd string) (PaneID, error)
    FindPanes(session string) (map[string]PaneID, error) // feature -> pane
    Show(feature PaneID, slot PaneID) error               // swap into slot
    Capture(p PaneID) (string, error)
    IsDead(p PaneID) (bool, error)
    Respawn(p PaneID, cmd string) error
    Kill(p PaneID) error
    Popup(cwd, cmd string, w, h int) error
    BindKey(table, key, cmd string) error
    UnbindKey(table, key string) error
}
```
v1 ships only the tmux implementation. Don't over-abstract beyond what tmux needs.

## 7. Package layout (builder may adjust)

```
cmd/agenttree/main.go   subcommands: (none)=run UI, ctl, hook, setup, new, settings, cleanup, placeholder
internal/config         load/merge/save TOML settings (global + per-repo), defaults
internal/state          state.json + lock
internal/gitwt          git: repo root, default branch, worktree add/list/remove, branch delete, ignore modes, dirty/unpushed checks
internal/setup          detect copyable files + dependency tooling; copy (cp -c) / install
internal/hooks          install per-agent hook config into worktrees; parse hook events
internal/mux            Multiplexer interface + tmux implementation
internal/activity       status state machine merging hook events + screen changes (pure, fake clock)
internal/pr             gh polling and parsing
internal/ctl            Unix-socket protocol for `agenttree ctl` / `agenttree hook` → UI
internal/ui             Bubble Tea: tab bar, sidebar, popups (Huh), onboarding, confirm dialogs
```

## 8. Requirements

- Go ≥ 1.24, tmux ≥ 3.2 (popups), git ≥ 2.20, `gh` optional (PR status),
  `lazygit` optional (git/diff popup).

## 9. Milestones

1. **tmux core:** start in tmux, split slot, background session, create panes,
   swap between two hard-coded agents. Proves the switching feels right.
2. Git/worktree layer + state file + onboarding/ignore modes (tested against temp repos).
3. Feature list UI (tabs + sidebar), new-feature popup, `lazygit` popup, tmux key bindings + ctl socket.
4. Worktree setup (copy / install) with first-feature popup.
5. Activity status: screen-change guess, then Claude Code + Codex hooks.
6. PR status + cleanup.
7. Reconnect/relaunch on restart.
8. Settings popup.

## 10. Open questions for the owner

None blocking. Resolved 2026-11-09: ignore mode (gitignore + commit, per-repo
onboarding), worktree setup (first-feature popup, then automatic), hooks in v1,
TOML, use the owner's tmux server, `lazygit` popup for active worktree (`prefix + G` / `g`).

## 11. Prior art

- **Herdr** (`herdrdev/herdr`): Single-binary Rust agent runtime and terminal
  multiplexer with background persistence, worktree workspaces, plugins, and
  native agent state tracking (`working`, `blocked`, `done`, `idle`) across
  Claude Code, Antigravity, Codex, and OpenCode via `OSC 7501` and agent hooks.
  Why we don't adopt it: it replaces `tmux` with its own multiplexer rather than
  living inside the owner's existing `tmux` workflow and defaults. We borrow its
  `Blocked` vs `Done` (unread) status split (§6.4) and can reference its agent
  hook definitions during implementation.
- **Claude Squad** (`smtg-ai/claude-squad`): Go + Bubble Tea + tmux + worktrees,
  very close to this idea. Owner's view: UI is bloated, and the project looks
  unmaintained (30+ open PRs, no updates in ~3 months). Worth studying for its
  tmux and worktree handling and for its mistakes, but we build our own.
- **workmux** (`raine/workmux`): Rust CLI + tmux + optional TUI. Closest
  philosophical peer (`.workmux.yaml` for `.env` copy, `node_modules` symlinks,
  post-create hooks, status in tmux window list). Maps one worktree to one
  *tmux window* rather than swapping an agent pane inside your existing window.
- **Conductor** (`conductor.build`, joining Meta): macOS GUI app. Strongest
  prior art on worktree setup (`.worktreeinclude` for ignored files, setup
  scripts, `CONDUCTOR_PORT` env injection per workspace) and diff-first PR
  review, but replaces the terminal with a standalone GUI.
- **Uzi** (`devflowinc/uzi`): Go CLI + tmux for running agent swarms in parallel
  worktrees with dynamic port allocation (`uzi.yaml`) and auto-confirmation
  (`uzi auto`). Batch CLI runner rather than an interactive pane switcher.
- **Crystal (now Nimbalyst)** & **Superset** (`superset.sh`): Desktop GUI
  workspaces with Kanban boards, prompt A/B testing across worktrees, and
  built-in diff/browser previews. Heavy IDEs, opposite of our minimal anti-goal.
- **dmux** (`dmux.ai`) & **loom** (`johnaparker/loom`): Terminal agent managers
  built on tmux. `dmux` adds LLM-generated branch names and desktop
  notifications; `loom` integrates Linear issues and GitHub PRs for Claude Code.
- **cmux** (`cmux.com`) & **Superlogical's "Rex"**: Next-generation terminal
  multiplexers built on `libghostty` (Rex is from Ghostty creator Mitchell
  Hashimoto's company Superlogical). Both feature persistent client-server
  sessions, native notification rings, and programmable socket/CLI APIs for
  agents—validating the `Multiplexer` interface in §6.12.
- **Worktree CLIs (`gwq`, `worktrunk` / `wt`) & `sesh`**: `worktrunk` (`wt`)
  pairs branch-centric worktree switching with `-x claude` agent handoff and
  `wt pr <num>` checkout; `gwq` uses TOML config for `.env` copy + setup; `sesh`
  popularized instant last-session toggling in tmux.
- **Claude Code native `--worktree`**: Built-in CLI flag (`claude --worktree`)
  with `WorktreeCreate` / `WorktreeRemove` lifecycle hooks, scoped only to
  Claude Code without a multi-agent switcher UI.

## 12. Future / deferred ideas (post-v1)

Noted from market research and design review; explicitly deferred out of v1:

1. **Per-feature port & env injection (`AGENTTREE_PORT`):** Deterministically
   assign a port offset per feature and export `AGENTTREE_PORT` /
   `AGENTTREE_WORKTREE_PATH` into the agent pane so parallel dev servers or E2E
   test runs don't collide on `localhost`.
2. **Initial prompt / issue / PR checkout in New Feature popup:** Optional
   initial prompt field passed straight to the agent on launch, auto-slugifying
   the branch name when omitted, plus `#123` / `pr:123` shorthand via `gh`.
3. **Symlink & Linux reflink support for dependencies:** Add `symlink` as a
   `setup.deps` option alongside `copy` and `install`, and use
   `cp -R --reflink=auto` on Linux where macOS `cp -c` isn't available.
4. **"Jump to Needs-You" & "Last Feature" quick-toggle keybindings:** Single
   keys to jump directly to the next 🔔 agent or bounce between the two most
   recent features (`sesh`-style).
5. **Terminal bell / desktop notification pass-through:** Emit a bell (`\a`) or
   OSC notification from the agenttree UI pane when a backgrounded feature transitions
   to `Needs you`.
6. **Background pane resize & post-swap hash debounce:** Keep background
   session window dimensions synced with the visible slot size and suppress
   screen-change hashing briefly after `swap-pane` so `SIGWINCH` redraws don't
   false-trigger `Working`.
7. **Richer Claude Code hooks (`PermissionRequest`, `PostToolUse`) & nested
   worktrees:** Catch tool permission prompts immediately (without waiting for
   `Notification` idle delay), flip back to `Working` on `PostToolUse`, and
   handle nested `.claude/worktrees/` on cleanup.
8. **Protected-branch check for `gitignore-commit`:** Warn or fall back to
   `.git/info/exclude` if the main checkout is on a protected default branch
   (`main`/`master`) during onboarding.
9. **Teardown hook (`setup.teardown_cmd`):** Optional pre-cleanup command (e.g.,
   `docker compose down`) run before removing a worktree.
