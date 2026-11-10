package ui

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
type focusActiveMsg struct{}

type Model struct {
	deps        AppDeps
	cfg         config.Config
	state       *state.State
	panes       map[string]mux.PaneID
	currentSlot mux.PaneID
	tracker     *activity.Tracker
	prStatuses  map[string]pr.Status
	ghAvail     bool
	width       int
	height      int
}

func NewModel(deps AppDeps, cfg config.Config, st *state.State) *Model {
	workingTimeout := time.Duration(cfg.Status.WorkingTimeoutSec) * time.Second
	if workingTimeout <= 0 {
		workingTimeout = 3 * time.Second
	}
	return &Model{
		deps:        deps,
		cfg:         cfg,
		state:       st,
		panes:       make(map[string]mux.PaneID),
		currentSlot: deps.SlotPane,
		tracker:     activity.NewTracker(workingTimeout, time.Now),
		prStatuses:  make(map[string]pr.Status),
		ghAvail:     true,
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

func (m *Model) CurrentSlot() mux.PaneID {
	return m.currentSlot
}

func (m *Model) ReconcileOnStart() error {
	_ = m.deps.Mux.EnsureSession(m.deps.BgSession)

	panes, err := m.deps.Mux.FindPanes(m.deps.BgSession)
	if err == nil {
		m.panes = panes
	}

	if m.currentSlot != "" && m.currentSlot != m.deps.SlotPane {
		slotAlive := false
		for _, pid := range m.panes {
			if pid == m.currentSlot {
				slotAlive = true
				break
			}
		}
		if !slotAlive {
			m.currentSlot = m.deps.SlotPane
		}
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
		m.showFeature(m.state.SelectedFeature, true)
	}

	return nil
}

func (m *Model) showFeature(name string, focusAgent bool) {
	p, ok := m.panes[name]
	if !ok || p == "" {
		return
	}
	slot := m.currentSlot
	if slot == "" {
		slot = m.deps.SlotPane
	}
	if err := m.deps.Mux.Show(p, slot); err == nil {
		m.currentSlot = p
	}
	m.tracker.MarkViewed(name)
	if !focusAgent && m.deps.SelfPane != "" {
		_ = m.deps.Mux.Show(m.deps.SelfPane, m.deps.SelfPane)
	}
}

func (m *Model) focusActivePane() {
	if m.state.SelectedFeature != "" {
		m.showFeature(m.state.SelectedFeature, true)
	} else if m.currentSlot != "" {
		_ = m.deps.Mux.Show(m.currentSlot, m.currentSlot)
	}
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

func delayedFocusCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return focusActiveMsg{}
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case focusActiveMsg:
		m.focusActivePane()
		return m, nil

	case tickMsg:
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
			if msg.Action == "reload" {
				return m, delayedFocusCmd()
			}
			return m, nil
		}

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			m.selectPrev(false)
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.selectNext(false)
			return m, nil
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			return m, m.handleMouseClick(msg.X, msg.Y)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter", "tab", "right", "l", " ":
			if len(m.state.Features) == 0 {
				m.openNewPopup()
			} else {
				m.focusActivePane()
			}
		case "j", "down":
			m.selectNext(false)
		case "k", "up":
			m.selectPrev(false)
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
				m.selectIndex(idx-1, true)
			}
		}
	}

	return m, nil
}

func (m *Model) handleMouseClick(x, y int) tea.Cmd {
	if m.cfg.Layout == "tabs" {
		if y <= 1 {
			m.handleTabClick(x)
		}
		return nil
	}

	n := len(m.state.Features)
	if n == 0 {
		if y >= 2 && y <= 4 {
			m.openNewPopup()
		} else if y == 5 {
			if x < 13 {
				m.openSettingsPopup()
			} else {
				return tea.Quit
			}
		}
		return nil
	}

	switch {
	case y >= 2 && y < 2+n:
		m.selectIndex(y-2, true)
	case y == 3+n:
		m.focusActivePane()
	case y == 4+n:
		m.openNewPopup()
	case y == 5+n:
		if x < 13 {
			m.openLazygit()
		} else {
			m.openCleanupPopup()
		}
	case y == 6+n:
		if x < 13 {
			m.restartActive()
		} else {
			m.openSettingsPopup()
		}
	case y == 7+n:
		return tea.Quit
	}
	return nil
}

func (m *Model) handleTabClick(x int) {
	col := 0
	for i, f := range m.state.Features {
		st := m.tracker.State(f.Name)
		tabText := " " + statusIcon(st) + " " + strconv.Itoa(i+1) + ":" + f.Name
		if prSt, ok := m.prStatuses[f.Branch]; ok {
			tabText += " [" + prSt.Display + "]"
		}
		tabText += " "
		w := lipgloss.Width(tabText)
		if x >= col && x < col+w {
			m.selectIndex(i, true)
			return
		}
		col += w
	}
	if x >= col {
		m.openNewPopup()
	}
}

func (m *Model) handleAction(action string) {
	switch {
	case action == "next":
		m.selectNext(true)
	case action == "prev":
		m.selectPrev(true)
	case action == "focus":
		m.focusActivePane()
	case strings.HasPrefix(action, "jump:"):
		parts := strings.Split(action, ":")
		if len(parts) == 2 {
			idx, _ := strconv.Atoi(parts[1])
			m.selectIndex(idx-1, true)
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
		if cfg, _, err := config.Load(config.GlobalConfigPath(), config.RepoConfigPath(m.deps.GitCommonDir)); err == nil {
			m.cfg = cfg
		}
		if st, err := state.Load(m.deps.GitCommonDir); err == nil {
			m.state = st
			_ = m.ReconcileOnStart()
		}
	}
}

func (m *Model) selectNext(focusAgent bool) {
	if len(m.state.Features) == 0 {
		return
	}
	curIdx := m.currentFeatureIndex()
	nextIdx := (curIdx + 1) % len(m.state.Features)
	m.selectIndex(nextIdx, focusAgent)
}

func (m *Model) selectPrev(focusAgent bool) {
	if len(m.state.Features) == 0 {
		return
	}
	curIdx := m.currentFeatureIndex()
	prevIdx := (curIdx - 1 + len(m.state.Features)) % len(m.state.Features)
	m.selectIndex(prevIdx, focusAgent)
}

func (m *Model) selectIndex(idx int, focusAgent bool) {
	if idx < 0 || idx >= len(m.state.Features) {
		return
	}
	target := m.state.Features[idx]
	m.state.SelectedFeature = target.Name
	_ = state.Save(m.deps.GitCommonDir, m.state)
	m.showFeature(target.Name, focusAgent)
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
		m.showFeature(feat.Name, true)
	}
}
