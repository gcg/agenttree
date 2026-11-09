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
