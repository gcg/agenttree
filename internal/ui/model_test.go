package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcg/agenttree/internal/config"
	"github.com/gcg/agenttree/internal/ctl"
	"github.com/gcg/agenttree/internal/mux"
	"github.com/gcg/agenttree/internal/state"
)

type fakeMux struct {
	mux.Multiplexer
	shownFeature mux.PaneID
	shownSlot    mux.PaneID
	showCount    int
	popups       []string
	panes        map[string]mux.PaneID
}

func (f *fakeMux) EnsureSession(name string) error                        { return nil }
func (f *fakeMux) FindPanes(session string) (map[string]mux.PaneID, error) { return f.panes, nil }
func (f *fakeMux) Show(feat, slot mux.PaneID) error {
	f.shownFeature = feat
	f.shownSlot = slot
	f.showCount++
	return nil
}
func (f *fakeMux) Popup(cwd, cmd string, w, h int) error { f.popups = append(f.popups, cwd+":"+cmd); return nil }
func (f *fakeMux) Capture(p mux.PaneID) (string, error)  { return "", nil }
func (f *fakeMux) IsDead(p mux.PaneID) (bool, error)     { return false, nil }
func (f *fakeMux) HasBell(p mux.PaneID) (bool, error)    { return false, nil }

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
	if m.CurrentSlot() != "%11" {
		t.Fatalf("expected CurrentSlot=%%11 after ReconcileOnStart, got %q", m.CurrentSlot())
	}

	// Next action switches to feat-2 and swaps pane %12 against current slot %11 (not initial %2).
	updated, _ := m.Update(ctl.Message{Kind: "action", Action: "next"})
	m = updated.(*Model)
	if m.SelectedFeature() != "feat-2" || fm.shownFeature != "%12" || fm.shownSlot != "%11" {
		t.Fatalf("expected feat-2 (%%12 into %%11), got selected=%q feat=%q slot=%q", m.SelectedFeature(), fm.shownFeature, fm.shownSlot)
	}

	// Pressing Enter in the sidebar focuses active pane (%12) with slot=%12 (no swap-out).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if fm.shownFeature != "%12" || fm.shownSlot != "%12" {
		t.Fatalf("expected Enter to focus %%12 (slot=%%12), got feat=%q slot=%q", fm.shownFeature, fm.shownSlot)
	}

	// Clicking row 2 (first feature: feat-1) with the mouse selects feat-1 and focuses %11.
	updated, _ = m.Update(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.SelectedFeature() != "feat-1" || fm.shownFeature != "%11" || fm.shownSlot != "%12" {
		t.Fatalf("expected mouse click on row 2 to select feat-1 (%%11), got selected=%q feat=%q slot=%q", m.SelectedFeature(), fm.shownFeature, fm.shownSlot)
	}

	// Lazygit action opens popup in /wt/feat-1.
	updated, _ = m.Update(ctl.Message{Kind: "action", Action: "lazygit"})
	m = updated.(*Model)
	if len(fm.popups) != 1 || !strings.HasPrefix(fm.popups[0], "/wt/feat-1:lazygit") {
		t.Fatalf("expected lazygit popup in /wt/feat-1, got %v", fm.popups)
	}

	// View renders both features and switching instructions in sidebar mode.
	view := m.View()
	if !strings.Contains(view, "feat-1") || !strings.Contains(view, "feat-2") || !strings.Contains(view, "Focus Agent") {
		t.Fatalf("expected view to contain feature names and switching help, got:\n%s", view)
	}
}
