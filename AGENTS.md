# AGENTS.md

Instructions for every coding agent working in this repo. `CLAUDE.md` and
`GEMINI.md` are symlinks to this file. Edit this file only.

## Project

agenttree is a Go terminal app (Charm: Bubble Tea, Lip Gloss, Huh) that manages
git worktrees and runs one coding agent per feature, using tmux to host the
agents. Design: [docs/superpowers/specs/2026-11-09-agenttree-design.md](docs/superpowers/specs/2026-11-09-agenttree-design.md).

## PR size

- One logical change per PR (one feature slice, one refactor, one bug fix). Never mix refactoring with behavior changes.
- Target under 300 changed lines; hard limit 500. Lockfiles, generated code, snapshots, and migrations don't count.
- Tests count but may push a PR past the target; that's fine.
- If a task will exceed the limit, split it into a sequence of PRs (stacked or merged in order), each of which builds and passes tests on its own. Propose the split plan before starting.
- Pure mechanical changes (renames, formatting, dependency bumps) can be larger but must be in their own PR and say so in the title.
