package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCLINotInTmux(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Env = append(os.Environ(), "TMUX=")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail when TMUX is not set, but succeeded: %s", string(out))
	}
	if !strings.Contains(string(out), "error: agenttree must be run inside tmux") {
		t.Fatalf("unexpected output: %s", string(out))
	}
}

func TestCLIHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help failed: %v, out: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage:") || !strings.Contains(string(out), "agenttree [run]") {
		t.Fatalf("unexpected help output: %s", string(out))
	}
}

func TestCLISetupMissingFeature(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "setup")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error for missing --feature, got success: %s", string(out))
	}
	if !strings.Contains(string(out), "missing --feature flag") {
		t.Fatalf("unexpected output: %s", string(out))
	}
}

func TestCLICtlMissingAction(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "ctl")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error for missing ctl action, got success: %s", string(out))
	}
	if !strings.Contains(string(out), "usage: agenttree ctl <action>") {
		t.Fatalf("unexpected output: %s", string(out))
	}
}
