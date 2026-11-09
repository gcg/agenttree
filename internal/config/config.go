package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type AgentCmd struct {
	Launch string `toml:"launch"`
	Resume string `toml:"resume"`
}

type KeysConfig struct {
	New      string `toml:"new"`
	Next     string `toml:"next"`
	Prev     string `toml:"prev"`
	Lazygit  string `toml:"lazygit"`
	Cleanup  string `toml:"cleanup"`
	Restart  string `toml:"restart"`
	Settings string `toml:"settings"`
	AltJump  bool   `toml:"alt_jump"`
}

type StatusConfig struct {
	WorkingTimeoutSec int `toml:"working_timeout"`
}

type PRConfig struct {
	PollIntervalSec int `toml:"poll_interval"`
}

type SetupConfig struct {
	Copy       []string `toml:"copy"`
	Deps       string   `toml:"deps"`
	InstallCmd string   `toml:"install_cmd"`
	Configured bool     `toml:"configured"`
}

type Config struct {
	Layout       string              `toml:"layout"`
	SidebarWidth int                 `toml:"sidebar_width"`
	DefaultAgent string              `toml:"default_agent"`
	BranchPrefix string              `toml:"branch_prefix"`
	WorktreeDir  string              `toml:"worktree_dir"`
	IgnoreMode   string              `toml:"ignore_mode"`
	Agents       map[string]AgentCmd `toml:"agents"`
	Keys         KeysConfig          `toml:"keys"`
	Status       StatusConfig        `toml:"status"`
	PR           PRConfig            `toml:"pr"`
	Setup        SetupConfig         `toml:"setup"`
}

func Default() Config {
	return Config{
		Layout:       "sidebar",
		SidebarWidth: 24,
		DefaultAgent: "claude",
		BranchPrefix: "",
		WorktreeDir:  ".worktrees",
		IgnoreMode:   "gitignore-commit",
		Agents: map[string]AgentCmd{
			"claude":   {Launch: "claude", Resume: "claude --continue"},
			"agy":      {Launch: "agy", Resume: "agy --continue"},
			"codex":    {Launch: "codex", Resume: "codex resume --last"},
			"opencode": {Launch: "opencode", Resume: "opencode --continue"},
		},
		Keys: KeysConfig{
			New:      "C",
			Next:     ")",
			Prev:     "(",
			Lazygit:  "G",
			Cleanup:  "X",
			Restart:  "R",
			Settings: "S",
			AltJump:  true,
		},
		Status: StatusConfig{
			WorkingTimeoutSec: 3,
		},
		PR: PRConfig{
			PollIntervalSec: 60,
		},
		Setup: SetupConfig{
			Copy:       []string{".env*"},
			Deps:       "none",
			InstallCmd: "",
			Configured: false,
		},
	}
}

func GlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "agenttree", "config.toml")
}

func RepoConfigPath(gitCommonDir string) string {
	return filepath.Join(gitCommonDir, "agenttree", "config.toml")
}

func Load(globalPath, repoPath string) (Config, bool, error) {
	cfg := Default()

	if globalPath != "" {
		if _, err := os.Stat(globalPath); err == nil {
			if _, err := toml.DecodeFile(globalPath, &cfg); err != nil {
				return cfg, false, fmt.Errorf("decode global config %q: %w", globalPath, err)
			}
		}
	}

	repoExists := false
	if repoPath != "" {
		if _, err := os.Stat(repoPath); err == nil {
			repoExists = true
			if _, err := toml.DecodeFile(repoPath, &cfg); err != nil {
				return cfg, false, fmt.Errorf("decode repo config %q: %w", repoPath, err)
			}
		}
	}

	// Ensure all standard agents are present if not overridden
	def := Default()
	if cfg.Agents == nil {
		cfg.Agents = def.Agents
	} else {
		for k, v := range def.Agents {
			if _, exists := cfg.Agents[k]; !exists {
				cfg.Agents[k] = v
			}
		}
	}

	return cfg, repoExists, nil
}

func Save(path string, cfg Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	enc := toml.NewEncoder(tmpFile)
	if err := enc.Encode(cfg); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("encode config toml: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp config to %q: %w", path, err)
	}

	return nil
}
