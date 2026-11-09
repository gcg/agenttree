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

func (f *fakeMux) EnsureSession(name string) error                        { return nil }
func (f *fakeMux) FindPanes(session string) (map[string]mux.PaneID, error) { return f.panes, nil }
func (f *fakeMux) Show(feat, slot mux.PaneID) error                        { f.shownFeature = feat; return nil }
func (f *fakeMux) Popup(cwd, cmd string, w, h int) error                   { f.popups = append(f.popups, cwd+":"+cmd); return nil }
func (f *fakeMux) Capture(p mux.PaneID) (string, error)                    { return "", nil }
func (f *fakeMux) IsDead(p mux.PaneID) (bool, error)                       { return false, nil }
func (f *fakeMux) HasBell(p mux.PaneID) (bool, error)                      { return false, nil }

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
		t.Fatalf("expected feat-2 (%%12), got selected=%q pane=%q", m.SelectedFeature(), fm.shownFeature)
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
