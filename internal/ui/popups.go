package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/gcg/agenttree/internal/config"
	"github.com/gcg/agenttree/internal/ctl"
	"github.com/gcg/agenttree/internal/gitwt"
	"github.com/gcg/agenttree/internal/hooks"
	"github.com/gcg/agenttree/internal/mux"
	"github.com/gcg/agenttree/internal/setup"
	"github.com/gcg/agenttree/internal/state"
)

func ValidateFeatureName(name, prefix string, existing []state.Feature) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" { return errors.New("feature name cannot be empty") }
	slug, branch, err := gitwt.Slugify(trimmed, prefix)
	if err != nil { return err }
	for _, f := range existing {
		if f.Name == slug || f.Branch == branch {
			return fmt.Errorf("feature %q (branch %q) already exists", slug, branch)
		}
	}
	return nil
}

func RunOnboardingPopup(repoRoot, gitCommonDir string) error {
	worktreeDir, ignoreMode := ".worktrees", "gitignore-commit"
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Worktree Directory").Description("Folder under repo where worktrees will live").Value(&worktreeDir),
		huh.NewSelect[string]().Title("How to ignore worktrees").Options(
			huh.NewOption("gitignore-commit (default, append to .gitignore & commit)", "gitignore-commit"),
			huh.NewOption("gitignore (append to .gitignore without commit)", "gitignore"),
			huh.NewOption("exclude (local only, write to .git/info/exclude)", "exclude"),
		).Value(&ignoreMode),
	))
	if err := form.Run(); err != nil {
		return err
	}
	applied, err := gitwt.EnsureIgnored(repoRoot, gitCommonDir, worktreeDir, ignoreMode)
	if err != nil {
		return fmt.Errorf("ensure ignored: %w", err)
	}
	repoCfgPath := config.RepoConfigPath(gitCommonDir)
	cfg, _, _ := config.Load("", repoCfgPath)
	cfg.WorktreeDir, cfg.IgnoreMode = worktreeDir, applied
	return config.Save(repoCfgPath, cfg)
}

func RunFirstSetupPopup(repoRoot, gitCommonDir string) error {
	candidates, _ := setup.DetectCopyCandidates(repoRoot)
	depInfo := setup.DetectDeps(repoRoot)
	var selectedFiles []string
	for _, c := range candidates {
		if strings.HasPrefix(c, ".env") {
			selectedFiles = append(selectedFiles, c)
		}
	}
	depsMode, installCmd := "copy", depInfo.InstallCmd
	if depInfo.CopyDir == "" && depInfo.InstallCmd != "" {
		depsMode = "install"
	}
	var fields []huh.Field
	if len(candidates) > 0 {
		var opts []huh.Option[string]
		for _, c := range candidates {
			opts = append(opts, huh.NewOption(c, c))
		}
		fields = append(fields, huh.NewMultiSelect[string]().Title("Files to copy to new worktrees").Options(opts...).Value(&selectedFiles))
	}
	fields = append(fields,
		huh.NewSelect[string]().Title("Dependencies Setup").Options(
			huh.NewOption("Copy existing directory ("+depInfo.CopyDir+", fast CoW clone)", "copy"),
			huh.NewOption("Run install command ("+depInfo.InstallCmd+")", "install"),
			huh.NewOption("None", "none"),
		).Value(&depsMode),
		huh.NewInput().Title("Install Command").Value(&installCmd),
	)
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return err
	}
	repoCfgPath := config.RepoConfigPath(gitCommonDir)
	cfg, _, _ := config.Load("", repoCfgPath)
	cfg.Setup.Copy, cfg.Setup.Deps, cfg.Setup.InstallCmd, cfg.Setup.Configured = selectedFiles, depsMode, installCmd, true
	return config.Save(repoCfgPath, cfg)
}

func RunNewFeaturePopup(repoRoot, gitCommonDir, bgSession, binPath, sockPath string, m mux.Multiplexer) error {
	globalPath, repoPath := config.GlobalConfigPath(), config.RepoConfigPath(gitCommonDir)
	cfg, _, err := config.Load(globalPath, repoPath)
	if err != nil {
		return err
	}
	if !cfg.Setup.Configured {
		if err := RunFirstSetupPopup(repoRoot, gitCommonDir); err != nil {
			return err
		}
		cfg, _, _ = config.Load(globalPath, repoPath)
	}
	st, err := state.Load(gitCommonDir)
	if err != nil {
		return err
	}
	featureName, selectedAgent := "", cfg.DefaultAgent
	if selectedAgent == "" {
		selectedAgent = "claude"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Feature Name").Validate(func(s string) error {
			return ValidateFeatureName(s, cfg.BranchPrefix, st.Features)
		}).Value(&featureName),
		huh.NewSelect[string]().Title("Agent").Options(
			huh.NewOption("Claude Code (claude)", "claude"),
			huh.NewOption("Antigravity (agy)", "agy"),
			huh.NewOption("Codex (codex)", "codex"),
			huh.NewOption("OpenCode (opencode)", "opencode"),
		).Value(&selectedAgent),
	))
	if err := form.Run(); err != nil {
		return err
	}
	slug, branch, err := gitwt.Slugify(featureName, cfg.BranchPrefix)
	if err != nil {
		return err
	}
	wtDir := cfg.WorktreeDir
	if wtDir == "" {
		wtDir = ".worktrees"
	}
	wtPath := filepath.Join(repoRoot, wtDir, slug)
	if err := gitwt.AddWorktree(repoRoot, wtPath, branch, gitwt.DefaultBranch(repoRoot)); err != nil {
		return fmt.Errorf("add worktree: %w", err)
	}
	_ = hooks.InstallWorktreeHooks(selectedAgent, wtPath, gitCommonDir, binPath)
	st.Upsert(state.Feature{Name: slug, Branch: branch, WorktreePath: wtPath, AgentID: selectedAgent, CreatedAt: time.Now()})
	st.SelectedFeature = slug
	_ = state.Save(gitCommonDir, st)

	agentCmd := cfg.Agents[selectedAgent].Launch
	if agentCmd == "" {
		agentCmd = selectedAgent
	}
	cmdStr := fmt.Sprintf("%s setup --feature %s && exec %s", binPath, slug, hooks.WrapAgentCommand(selectedAgent, agentCmd, binPath))
	_, err = m.NewAgentPane(bgSession, slug, wtPath, cmdStr, map[string]string{"AGENTTREE_FEATURE": slug, "AGENTTREE_CTL": sockPath})
	_ = ctl.Send(sockPath, ctl.Message{Kind: "action", Action: "reload"})
	return err
}

func RunCleanupPopup(repoRoot, gitCommonDir, featureName string, prMerged bool, m mux.Multiplexer, bgSession string) error {
	st, err := state.Load(gitCommonDir)
	if err != nil {
		return err
	}
	feat, ok := st.Find(featureName)
	if !ok {
		return fmt.Errorf("feature %q not found", featureName)
	}
	safety, err := gitwt.CheckSafety(repoRoot, feat.WorktreePath, feat.Branch, prMerged)
	if err != nil {
		return fmt.Errorf("safety check: %w", err)
	}
	confirm := false
	desc := fmt.Sprintf("Clean up feature %q:\n• Stop agent pane\n• Remove worktree %s\n• Delete branch %s", featureName, feat.WorktreePath, feat.Branch)
	title := "Proceed with cleanup?"
	if safety.NeedsForce() {
		title = "Force clean up anyway?"
		var warn []string
		if safety.Dirty { warn = append(warn, "- Uncommitted changes in worktree") }
		if safety.Unmerged { warn = append(warn, "- Unmerged commits on branch") }
		if safety.Unpushed { warn = append(warn, "- Unpushed commits to remote") }
		desc += "\n\n⚠️  Cannot clean up cleanly:\n" + strings.Join(warn, "\n")
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).Description(desc).Value(&confirm))).Run(); err != nil || !confirm {
		return err
	}
	panes, _ := m.FindPanes(bgSession)
	if p, ok := panes[featureName]; ok {
		_ = m.Kill(p)
	}
	if err := gitwt.RemoveWorktree(repoRoot, feat.WorktreePath, feat.Branch, safety.NeedsForce()); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	st.Remove(featureName)
	_ = state.Save(gitCommonDir, st)
	_ = ctl.Send(ctl.SocketPath(gitCommonDir), ctl.Message{Kind: "action", Action: "reload"})
	return nil
}

func RunSettingsPopup(repoRoot, gitCommonDir string) error {
	repoPath := config.RepoConfigPath(gitCommonDir)
	cfg, _, err := config.Load(config.GlobalConfigPath(), repoPath)
	if err != nil {
		return err
	}
	sidebarWidthStr := strconv.Itoa(cfg.SidebarWidth)
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Layout").Options(
			huh.NewOption("Sidebar (narrow pane on left)", "sidebar"),
			huh.NewOption("Tabs (short pane on top)", "tabs"),
		).Value(&cfg.Layout),
		huh.NewSelect[string]().Title("Default Agent").Options(
			huh.NewOption("Claude Code (claude)", "claude"),
			huh.NewOption("Antigravity (agy)", "agy"),
			huh.NewOption("Codex (codex)", "codex"),
			huh.NewOption("OpenCode (opencode)", "opencode"),
		).Value(&cfg.DefaultAgent),
		huh.NewInput().Title("Sidebar Width").Validate(func(s string) error {
			w, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || w <= 0 {
				return errors.New("must be a positive integer")
			}
			return nil
		}).Value(&sidebarWidthStr),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if w, err := strconv.Atoi(strings.TrimSpace(sidebarWidthStr)); err == nil && w > 0 {
		cfg.SidebarWidth = w
	}
	if err := config.Save(repoPath, cfg); err != nil {
		return err
	}
	_ = ctl.Send(ctl.SocketPath(gitCommonDir), ctl.Message{Kind: "action", Action: "reload"})
	return nil
}

