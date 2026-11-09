package ui

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcg/agenttree/internal/activity"
	"github.com/gcg/agenttree/internal/config"
	"github.com/gcg/agenttree/internal/ctl"
	"github.com/gcg/agenttree/internal/gitwt"
	"github.com/gcg/agenttree/internal/hooks"
	"github.com/gcg/agenttree/internal/mux"
	"github.com/gcg/agenttree/internal/pr"
	"github.com/gcg/agenttree/internal/state"
)

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

type tickMsg time.Time
type prTickMsg time.Time

type Model struct {
	deps       AppDeps
	cfg        config.Config
	state      *state.State
	panes      map[string]mux.PaneID
	tracker    *activity.Tracker
	prStatuses map[string]pr.Status
	ghAvail    bool
	width      int
	height     int
}

func NewModel(deps AppDeps, cfg config.Config, st *state.State) *Model {
	workingTimeout := time.Duration(cfg.Status.WorkingTimeoutSec) * time.Second
	if workingTimeout <= 0 {
		workingTimeout = 3 * time.Second
	}
	return &Model{
		deps:       deps,
		cfg:        cfg,
		state:      st,
		panes:      make(map[string]mux.PaneID),
		tracker:    activity.NewTracker(workingTimeout, time.Now),
		prStatuses: make(map[string]pr.Status),
		ghAvail:    true,
	}
}

func (m *Model) SelectedFeature() string {
	return m.state.SelectedFeature
}

func (m *Model) SelectedPane() mux.PaneID {
	if m.state == nil || m.state.SelectedFeature == "" {
		return ""
	}
	return m.panes[m.state.SelectedFeature]
}

func (m *Model) ReconcileOnStart() error {
	_ = m.deps.Mux.EnsureSession(m.deps.BgSession)

	panes, err := m.deps.Mux.FindPanes(m.deps.BgSession)
	if err == nil {
		m.panes = panes
	}

	// Discover any manual worktrees
	wtDir := m.cfg.WorktreeDir
	if wtDir == "" {
		wtDir = ".worktrees"
	}
	wts, err := gitwt.ListWorktrees(m.deps.RepoRoot, filepath.Join(m.deps.RepoRoot, wtDir))
	if err == nil {
		for _, w := range wts {
			if _, found := m.state.Find(w.Branch); !found {
				m.state.Upsert(state.Feature{
					Name:         w.Branch,
					Branch:       w.Branch,
					WorktreePath: w.Path,
					AgentID:      m.cfg.DefaultAgent,
					CreatedAt:    time.Now(),
				})
			}
		}
	}

	// Resume / launch missing panes
	for _, f := range m.state.Features {
		if _, ok := m.panes[f.Name]; !ok {
			agentCmd := m.cfg.Agents[f.AgentID].Resume
			if agentCmd == "" {
				agentCmd = m.cfg.Agents[f.AgentID].Launch
			}
			wrapped := hooks.WrapAgentCommand(f.AgentID, agentCmd, m.deps.BinPath)
			env := map[string]string{
				"AGENTTREE_FEATURE": f.Name,
				"AGENTTREE_CTL":     m.deps.SockPath,
			}
			paneID, err := m.deps.Mux.NewAgentPane(m.deps.BgSession, f.Name, f.WorktreePath, wrapped, env)
			if err == nil {
				m.panes[f.Name] = paneID
			}
		}
	}

	if m.state.SelectedFeature == "" && len(m.state.Features) > 0 {
		m.state.SelectedFeature = m.state.Features[0].Name
	}

	if m.state.SelectedFeature != "" {
		if p, ok := m.panes[m.state.SelectedFeature]; ok {
			_ = m.deps.Mux.Show(p, m.deps.SlotPane)
			m.tracker.MarkViewed(m.state.SelectedFeature)
		}
	}

	return nil
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		prTickCmd(m.cfg.PR.PollIntervalSec),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func prTickCmd(intervalSec int) tea.Cmd {
	if intervalSec <= 0 {
		intervalSec = 60
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return prTickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Poll activity of panes
		for featName, paneID := range m.panes {
			dead, _ := m.deps.Mux.IsDead(paneID)
			bell, _ := m.deps.Mux.HasBell(paneID)
			capture, _ := m.deps.Mux.Capture(paneID)
			isViewed := (featName == m.state.SelectedFeature)
			m.tracker.OnPoll(featName, capture, dead, bell, isViewed)
		}
		return m, tickCmd()

	case prTickMsg:
		prs, avail, err := pr.FetchAll(m.deps.RepoRoot)
		if err == nil {
			m.prStatuses = prs
			m.ghAvail = avail
		}
		return m, prTickCmd(m.cfg.PR.PollIntervalSec)

	case ctl.Message:
		if msg.Kind == "hook" {
			isViewed := (msg.Feature == m.state.SelectedFeature)
			m.tracker.OnHook(msg.Feature, msg.Event, isViewed)
			return m, nil
		}
		if msg.Kind == "action" {
			m.handleAction(msg.Action)
			return m, nil
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.selectNext()
		case "k", "up":
			m.selectPrev()
		case "g":
			m.openLazygit()
		case "c":
			m.openNewPopup()
		case "s":
			m.openSettingsPopup()
		case "x":
			m.openCleanupPopup()
		case "r":
			m.restartActive()
		default:
			if len(msg.String()) == 1 && msg.String() >= "1" && msg.String() <= "9" {
				idx, _ := strconv.Atoi(msg.String())
				m.selectIndex(idx - 1)
			}
		}
	}

	return m, nil
}

func (m *Model) handleAction(action string) {
	switch {
	case action == "next":
		m.selectNext()
	case action == "prev":
		m.selectPrev()
	case strings.HasPrefix(action, "jump:"):
		parts := strings.Split(action, ":")
		if len(parts) == 2 {
			idx, _ := strconv.Atoi(parts[1])
			m.selectIndex(idx - 1)
		}
	case action == "lazygit":
		m.openLazygit()
	case action == "cleanup":
		m.openCleanupPopup()
	case action == "new":
		m.openNewPopup()
	case action == "settings":
		m.openSettingsPopup()
	case action == "restart":
		m.restartActive()
	case action == "reload":
		if st, err := state.Load(m.deps.GitCommonDir); err == nil {
			m.state = st
			_ = m.ReconcileOnStart()
		}
	}
}

func (m *Model) selectNext() {
	if len(m.state.Features) == 0 {
		return
	}
	curIdx := m.currentFeatureIndex()
	nextIdx := (curIdx + 1) % len(m.state.Features)
	m.selectIndex(nextIdx)
}

func (m *Model) selectPrev() {
	if len(m.state.Features) == 0 {
		return
	}
	curIdx := m.currentFeatureIndex()
	prevIdx := (curIdx - 1 + len(m.state.Features)) % len(m.state.Features)
	m.selectIndex(prevIdx)
}

func (m *Model) selectIndex(idx int) {
	if idx < 0 || idx >= len(m.state.Features) {
		return
	}
	target := m.state.Features[idx]
	m.state.SelectedFeature = target.Name
	_ = state.Save(m.deps.GitCommonDir, m.state)

	if p, ok := m.panes[target.Name]; ok {
		_ = m.deps.Mux.Show(p, m.deps.SlotPane)
		m.tracker.MarkViewed(target.Name)
	}
}

func (m *Model) currentFeatureIndex() int {
	for i, f := range m.state.Features {
		if f.Name == m.state.SelectedFeature {
			return i
		}
	}
	return 0
}

func (m *Model) openLazygit() {
	cur, ok := m.state.Find(m.state.SelectedFeature)
	if !ok || cur.WorktreePath == "" {
		return
	}
	_ = m.deps.Mux.Popup(cur.WorktreePath, "lazygit", 90, 90)
}

func (m *Model) openNewPopup() {
	cmd := m.deps.BinPath + " new"
	_ = m.deps.Mux.Popup(m.deps.RepoRoot, cmd, 80, 80)
}

func (m *Model) openSettingsPopup() {
	cmd := m.deps.BinPath + " settings"
	_ = m.deps.Mux.Popup(m.deps.RepoRoot, cmd, 80, 80)
}

func (m *Model) openCleanupPopup() {
	if m.state.SelectedFeature == "" {
		return
	}
	cmd := m.deps.BinPath + " cleanup --feature " + m.state.SelectedFeature
	_ = m.deps.Mux.Popup(m.deps.RepoRoot, cmd, 80, 80)
}

func (m *Model) restartActive() {
	feat, ok := m.state.Find(m.state.SelectedFeature)
	if !ok {
		return
	}
	if p, ok := m.panes[feat.Name]; ok {
		agentCmd := m.cfg.Agents[feat.AgentID].Resume
		if agentCmd == "" {
			agentCmd = m.cfg.Agents[feat.AgentID].Launch
		}
		wrapped := hooks.WrapAgentCommand(feat.AgentID, agentCmd, m.deps.BinPath)
		_ = m.deps.Mux.Respawn(p, wrapped)
	}
}
