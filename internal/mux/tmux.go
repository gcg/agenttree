package mux

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Runner func(args ...string) (string, error)

type TmuxMux struct {
	Run      Runner
	RepoRoot string
}

func defaultRunner(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func NewTmuxMux(repoRoot string) *TmuxMux {
	return &TmuxMux{
		Run:      defaultRunner,
		RepoRoot: repoRoot,
	}
}

func InTmux() bool {
	return os.Getenv("TMUX") != ""
}

func BackgroundSessionName(repoRoot string) string {
	sum := sha256.Sum256([]byte(repoRoot))
	cleanBase := filepath.Base(filepath.Clean(repoRoot))
	return fmt.Sprintf("_agenttree-%s-%x", cleanBase, sum[:2])
}

func (m *TmuxMux) EnsureSession(name string) error {
	_, err := m.Run("has-session", "-t", "="+name)
	if err != nil {
		_, err = m.Run("new-session", "-d", "-s", name)
	}
	return err
}

func (m *TmuxMux) NewAgentPane(session, feature, cwd, cmdStr string, env map[string]string) (PaneID, error) {
	args := []string{"new-window", "-d", "-t", session + ":", "-n", feature, "-c", cwd, "-P", "-F", "#{pane_id}"}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, cmdStr)

	out, err := m.Run(args...)
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w (%s)", err, strings.TrimSpace(out))
	}

	paneID := PaneID(strings.TrimSpace(out))
	_, _ = m.Run("set-option", "-p", "-t", string(paneID), "remain-on-exit", "on")
	_, _ = m.Run("set-option", "-p", "-t", string(paneID), "@agenttree_feature", feature)
	_, _ = m.Run("set-option", "-p", "-t", string(paneID), "@agenttree_repo", m.RepoRoot)

	return paneID, nil
}

func (m *TmuxMux) FindPanes(session string) (map[string]PaneID, error) {
	panes := make(map[string]PaneID)

	// Panes in background session
	out, _ := m.Run("list-panes", "-s", "-t", session, "-F", "#{pane_id}\t#{@agenttree_feature}")
	parsePanes(out, panes)

	// Panes in current window (in case one is swapped into active slot)
	curOut, _ := m.Run("list-panes", "-F", "#{pane_id}\t#{@agenttree_feature}")
	parsePanes(curOut, panes)

	return panes, nil
}

func parsePanes(out string, dest map[string]PaneID) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			id := PaneID(strings.TrimSpace(parts[0]))
			feature := strings.TrimSpace(parts[1])
			if id != "" && feature != "" {
				dest[feature] = id
			}
		}
	}
}

func (m *TmuxMux) Show(feature PaneID, slot PaneID) error {
	if _, err := m.Run("swap-pane", "-d", "-s", string(feature), "-t", string(slot)); err != nil {
		return fmt.Errorf("tmux swap-pane: %w", err)
	}
	if _, err := m.Run("select-pane", "-t", string(slot)); err != nil {
		return fmt.Errorf("tmux select-pane: %w", err)
	}
	return nil
}

func (m *TmuxMux) Capture(p PaneID) (string, error) {
	out, err := m.Run("capture-pane", "-p", "-t", string(p))
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	return out, nil
}

func (m *TmuxMux) HasBell(p PaneID) (bool, error) {
	out, err := m.Run("display-message", "-p", "-t", string(p), "#{window_bell_flag}")
	if err != nil {
		return false, fmt.Errorf("tmux display-message bell: %w", err)
	}
	return strings.TrimSpace(out) == "1", nil
}

func (m *TmuxMux) IsDead(p PaneID) (bool, error) {
	out, err := m.Run("display-message", "-p", "-t", string(p), "#{pane_dead}")
	if err != nil {
		return false, fmt.Errorf("tmux display-message dead: %w", err)
	}
	return strings.TrimSpace(out) == "1", nil
}

func (m *TmuxMux) Respawn(p PaneID, cmd string) error {
	var args []string
	if cmd != "" {
		args = []string{"respawn-pane", "-k", "-t", string(p), cmd}
	} else {
		args = []string{"respawn-pane", "-k", "-t", string(p)}
	}
	_, err := m.Run(args...)
	return err
}

func (m *TmuxMux) Kill(p PaneID) error {
	_, err := m.Run("kill-pane", "-t", string(p))
	return err
}

func (m *TmuxMux) Popup(cwd, cmd string, wPct, hPct int) error {
	args := []string{"display-popup", "-E", "-w", fmt.Sprintf("%d%%", wPct), "-h", fmt.Sprintf("%d%%", hPct)}
	if cwd != "" {
		args = append(args, "-d", cwd)
	}
	args = append(args, cmd)
	_, err := m.Run(args...)
	return err
}

func (m *TmuxMux) BindKey(table, key, cmdStr string) (bool, error) {
	out, _ := m.Run("list-keys", "-T", table, key)
	trimmed := strings.TrimSpace(out)
	if trimmed != "" && !strings.Contains(trimmed, "agenttree ctl") {
		return true, nil
	}

	_, err := m.Run("bind-key", "-T", table, key, "run-shell", cmdStr)
	return false, err
}

func (m *TmuxMux) UnbindKey(table, key string) error {
	_, err := m.Run("unbind-key", "-T", table, key)
	return err
}

func (m *TmuxMux) SetupWindowSlot(layout string, size int, placeholderCmd, sockPath string) (PaneID, PaneID, string, error) {
	selfOut, err := m.Run("display-message", "-p", "#{pane_id}")
	if err != nil {
		return "", "", "", fmt.Errorf("get current pane: %w", err)
	}
	selfPane := PaneID(strings.TrimSpace(selfOut))

	winOut, err := m.Run("display-message", "-p", "#{window_id}")
	if err != nil {
		return "", "", "", fmt.Errorf("get current window: %w", err)
	}
	windowID := strings.TrimSpace(winOut)

	_, _ = m.Run("set-option", "-w", "-t", windowID, "@agenttree_ctl", sockPath)

	var splitArgs []string
	if layout == "tabs" {
		// tabs: short pane on top, agent slot below
		splitArgs = []string{"split-window", "-v", "-P", "-F", "#{pane_id}"}
	} else {
		// sidebar: narrow pane on left, agent slot on right
		splitArgs = []string{"split-window", "-h", "-P", "-F", "#{pane_id}"}
	}
	if placeholderCmd != "" {
		splitArgs = append(splitArgs, placeholderCmd)
	}

	slotOut, err := m.Run(splitArgs...)
	if err != nil {
		return "", "", "", fmt.Errorf("split agent slot: %w", err)
	}
	slotPane := PaneID(strings.TrimSpace(slotOut))

	// Adjust sidebar width or tab height
	if layout == "tabs" {
		if size > 0 {
			_, _ = m.Run("resize-pane", "-t", string(selfPane), "-y", strconv.Itoa(size))
		}
	} else {
		if size > 0 {
			_, _ = m.Run("resize-pane", "-t", string(selfPane), "-x", strconv.Itoa(size))
		}
	}

	// Re-focus self pane
	_, _ = m.Run("select-pane", "-t", string(selfPane))

	return selfPane, slotPane, windowID, nil
}

func (m *TmuxMux) CleanupWindow(slotPane, activeFeaturePane PaneID, bgSession, windowID string) error {
	if activeFeaturePane != "" && slotPane != "" {
		_, _ = m.Run("swap-pane", "-d", "-s", string(activeFeaturePane), "-t", string(slotPane))
	}
	if slotPane != "" {
		_, _ = m.Run("kill-pane", "-t", string(slotPane))
	}
	if windowID != "" {
		_, _ = m.Run("set-option", "-w", "-u", "-t", windowID, "@agenttree_ctl")
	}

	// Check if any other windows in the server are tagged with @agenttree_ctl
	out, _ := m.Run("list-windows", "-a", "-F", "#{@agenttree_ctl}")
	hasOther := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			hasOther = true
			break
		}
	}

	if !hasOther {
		// Unbind global prefix bindings
		for _, key := range []string{"C", ")", "(", "G", "X", "R", "S"} {
			_ = m.UnbindKey("prefix", key)
		}
		for i := 1; i <= 9; i++ {
			_ = m.UnbindKey("root", fmt.Sprintf("M-%d", i))
		}
	}

	return nil
}
