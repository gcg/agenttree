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
	// After swapping %5 and %2, %5 is in the window slot, so select-pane must target %5.
	if calls[1][2] != "%5" {
		t.Fatalf("expected select-pane target %%5, got %v", calls[1])
	}

	// Showing %5 when it is already in the slot (%5 == %5) must only select-pane without swapping.
	calls = nil
	if err := m.Show("%5", "%5"); err != nil {
		t.Fatalf("Show same pane: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "select-pane" || calls[0][2] != "%5" {
		t.Fatalf("expected only select-pane -t %%5 when already in slot, got %v", calls)
	}
}
