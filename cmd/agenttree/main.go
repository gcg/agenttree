package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/gcg/agenttree/internal/config"
	"github.com/gcg/agenttree/internal/ctl"
	"github.com/gcg/agenttree/internal/gitwt"
	"github.com/gcg/agenttree/internal/mux"
	"github.com/gcg/agenttree/internal/pr"
	"github.com/gcg/agenttree/internal/setup"
	"github.com/gcg/agenttree/internal/state"
	"github.com/gcg/agenttree/internal/ui"
)

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func mustResolveRepo() (string, string) {
	root, common, err := gitwt.ResolveRepo(".")
	if err != nil {
		die("error: %v", err)
	}
	return root, common
}

func selfBin() string {
	b, err := os.Executable()
	if err != nil {
		return "agenttree"
	}
	return b
}

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "ctl": runCtl(os.Args[2:])
	case "hook": runHook(os.Args[2:])
	case "setup": runSetup(os.Args[2:])
	case "new": runNew()
	case "settings": runSettings()
	case "cleanup": runCleanup(os.Args[2:])
	case "placeholder": runPlaceholder()
	case "help", "-h", "--help": printHelp()
	default: runRoot()
	}
}

func printHelp() {
	fmt.Println(`agenttree - manage git worktrees with parallel coding agents

Usage:
  agenttree [run]          Start agenttree UI in current tmux window
  agenttree ctl <action>   Send control action to agenttree (e.g. next, prev, lazygit)
  agenttree hook <event>   Send agent hook event (UserPromptSubmit, PreToolUse, etc.)
  agenttree setup          Run setup scripts and copy files for a feature
  agenttree new            Open new feature popup
  agenttree settings       Open settings popup
  agenttree cleanup        Open cleanup popup
  agenttree placeholder    Run placeholder pane while waiting for features`)
}

func runRoot() {
	if !mux.InTmux() {
		die("error: agenttree must be run inside tmux ($TMUX is not set)")
	}
	repoRoot, gitCommonDir := mustResolveRepo()

	tmuxMux := mux.NewTmuxMux(repoRoot)
	curPaneOut, err := tmuxMux.Run("display-message", "-p", "#{pane_id}")
	if err != nil {
		die("error getting current pane: %v", err)
	}

	releaseLock, err := state.AcquireLock(gitCommonDir, strings.TrimSpace(curPaneOut))
	if err != nil {
		die("%v", err)
	}
	defer releaseLock()

	repoCfgPath := config.RepoConfigPath(gitCommonDir)
	if _, err := os.Stat(repoCfgPath); os.IsNotExist(err) {
		if err := ui.RunOnboardingPopup(repoRoot, gitCommonDir); err != nil && !errors.Is(err, huh.ErrUserAborted) {
			die("onboarding error: %v", err)
		}
	}

	cfg, _, err := config.Load(config.GlobalConfigPath(), repoCfgPath)
	if err != nil {
		die("load config error: %v", err)
	}
	st, err := state.Load(gitCommonDir)
	if err != nil {
		die("load state error: %v", err)
	}

	sockPath := ctl.SocketPath(gitCommonDir)
	srv, err := ctl.StartServer(sockPath)
	if err != nil {
		die("start ctl server: %v", err)
	}
	defer srv.Close()

	binPath := selfBin()
	layout, size := cfg.Layout, cfg.SidebarWidth
	if layout == "" { layout = "sidebar" }
	if size <= 0 { size = 24 }
	if layout == "tabs" { size = 2 }

	selfPane, slotPane, windowID, err := tmuxMux.SetupWindowSlot(layout, size, binPath+" placeholder", sockPath)
	if err != nil {
		die("setup window slot: %v", err)
	}

	for _, b := range []struct{ k, a string }{
		{"C", "new"}, {")", "next"}, {"(", "prev"}, {"G", "lazygit"}, {"X", "cleanup"}, {"R", "restart"}, {"S", "settings"},
	} {
		_, _ = tmuxMux.BindKey("prefix", b.k, fmt.Sprintf("%s ctl %s --window #{window_id}", binPath, b.a))
	}
	for i := 1; i <= 9; i++ {
		_, _ = tmuxMux.BindKey("prefix", strconv.Itoa(i), fmt.Sprintf("%s ctl jump:%d --window #{window_id}", binPath, i))
	}

	bgSession := mux.BackgroundSessionName(repoRoot)
	m := ui.NewModel(ui.AppDeps{
		RepoRoot: repoRoot, GitCommonDir: gitCommonDir, SelfPane: selfPane, SlotPane: slotPane,
		WindowID: windowID, BgSession: bgSession, SockPath: sockPath, BinPath: binPath, Mux: tmuxMux,
	}, cfg, st)
	_ = m.ReconcileOnStart()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	go func() {
		for msg := range srv.Messages() {
			p.Send(msg)
		}
	}()

	_, _ = p.Run()
	_ = tmuxMux.CleanupWindow(slotPane, m.SelectedPane(), bgSession, windowID)
}

func runCtl(args []string) {
	var action, winID string
	for i := 0; i < len(args); i++ {
		if args[i] == "--window" && i+1 < len(args) {
			winID = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "-") && action == "" {
			action = args[i]
		}
	}
	if action == "" {
		die("usage: agenttree ctl <action> [--window <id>]")
	}

	sockPath := ""
	if winID != "" {
		out, err := exec.Command("tmux", "show-options", "-w", "-v", "-t", winID, "@agenttree_ctl").Output()
		sockPath = strings.TrimSpace(string(out))
		if err != nil || sockPath == "" {
			_ = exec.Command("tmux", "display-message", "no agenttree in this window").Run()
			return
		}
	} else if mux.InTmux() {
		out, _ := exec.Command("tmux", "show-options", "-w", "-v", "@agenttree_ctl").Output()
		sockPath = strings.TrimSpace(string(out))
	}
	if sockPath == "" {
		if _, gitCommonDir, err := gitwt.ResolveRepo("."); err == nil {
			sockPath = ctl.SocketPath(gitCommonDir)
		}
	}
	if sockPath == "" {
		die("cannot find agenttree socket")
	}
	if err := ctl.Send(sockPath, ctl.Message{Kind: "action", Action: action}); err != nil {
		die("ctl error: %v", err)
	}
}

func runHook(args []string) {
	if len(args) > 0 {
		if sock, feat := os.Getenv("AGENTTREE_CTL"), os.Getenv("AGENTTREE_FEATURE"); sock != "" && feat != "" {
			_ = ctl.Send(sock, ctl.Message{Kind: "hook", Feature: feat, Event: args[0]})
		}
	}
}

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	feature := fs.String("feature", "", "feature name")
	_ = fs.Parse(args)
	if *feature == "" {
		die("missing --feature flag")
	}
	repoRoot, gitCommonDir := mustResolveRepo()
	cfg, _, _ := config.Load(config.GlobalConfigPath(), config.RepoConfigPath(gitCommonDir))
	st, err := state.Load(gitCommonDir)
	if err != nil {
		die("load state: %v", err)
	}
	feat, ok := st.Find(*feature)
	if !ok {
		die("feature %q not found in state", *feature)
	}
	if err := setup.Run(repoRoot, feat.WorktreePath, cfg.Setup, os.Stdout); err != nil {
		die("setup error: %v", err)
	}
}

func runNew() {
	repoRoot, gitCommonDir := mustResolveRepo()
	if err := ui.RunNewFeaturePopup(repoRoot, gitCommonDir, mux.BackgroundSessionName(repoRoot), selfBin(), ctl.SocketPath(gitCommonDir), mux.NewTmuxMux(repoRoot)); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		die("new feature: %v", err)
	}
}

func runSettings() {
	_, gitCommonDir := mustResolveRepo()
	if err := ui.RunSettingsPopup(".", gitCommonDir); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		die("settings: %v", err)
	}
}

func runCleanup(args []string) {
	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	featFlag := fs.String("feature", "", "feature name")
	_ = fs.Parse(args)

	repoRoot, gitCommonDir := mustResolveRepo()
	st, err := state.Load(gitCommonDir)
	if err != nil {
		die("load state: %v", err)
	}
	target := *featFlag
	if target == "" {
		target = st.SelectedFeature
	}
	if target == "" {
		die("no feature to clean up")
	}
	prMerged := false
	if prs, _, err := pr.FetchAll(repoRoot); err == nil {
		if p, ok := prs[target]; ok && p.IsMerged {
			prMerged = true
		}
	}
	if err := ui.RunCleanupPopup(repoRoot, gitCommonDir, target, prMerged, mux.NewTmuxMux(repoRoot), mux.BackgroundSessionName(repoRoot)); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		die("cleanup: %v", err)
	}
}

func runPlaceholder() {
	fmt.Println("\n  Press prefix + C (or 'c') to start a feature")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
}
