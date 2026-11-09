package setup

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/gcg/agenttree/internal/config"
)

type DepInfo struct {
	Lockfile   string
	CopyDir    string
	InstallCmd string
}

func DetectCopyCandidates(repoRoot string) ([]string, error) {
	patterns := []string{
		".env",
		".env.*",
		".envrc",
		".tool-versions",
		"*.local.*",
		"*.local",
	}

	seen := make(map[string]bool)
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(repoRoot, p))
		if err != nil {
			continue
		}
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err == nil && !fi.IsDir() {
				rel, err := filepath.Rel(repoRoot, m)
				if err == nil {
					seen[rel] = true
				}
			}
		}
	}

	var candidates []string
	for c := range seen {
		candidates = append(candidates, c)
	}
	sort.Strings(candidates)
	return candidates, nil
}

func DetectDeps(repoRoot string) DepInfo {
	check := func(filename string) bool {
		_, err := os.Stat(filepath.Join(repoRoot, filename))
		return err == nil
	}

	switch {
	case check("pnpm-lock.yaml"):
		return DepInfo{Lockfile: "pnpm-lock.yaml", CopyDir: "node_modules", InstallCmd: "pnpm install"}
	case check("yarn.lock"):
		return DepInfo{Lockfile: "yarn.lock", CopyDir: "node_modules", InstallCmd: "yarn install"}
	case check("package-lock.json"):
		return DepInfo{Lockfile: "package-lock.json", CopyDir: "node_modules", InstallCmd: "npm ci"}
	case check("bun.lock") || check("bun.lockb"):
		return DepInfo{Lockfile: "bun.lock", CopyDir: "node_modules", InstallCmd: "bun install"}
	case check("composer.lock") || check("composer.json"):
		return DepInfo{Lockfile: "composer.lock", CopyDir: "vendor", InstallCmd: "composer install"}
	case check("Gemfile.lock"):
		return DepInfo{Lockfile: "Gemfile.lock", CopyDir: "vendor", InstallCmd: "bundle install"}
	case check("uv.lock"):
		return DepInfo{Lockfile: "uv.lock", CopyDir: ".venv", InstallCmd: "uv sync"}
	case check("go.mod"):
		return DepInfo{Lockfile: "go.mod", CopyDir: "", InstallCmd: "go mod download"}
	default:
		return DepInfo{}
	}
}

func Run(repoRoot, worktreePath string, cfg config.SetupConfig, out io.Writer) error {
	// 1. Copy configured files (never overwrite)
	for _, pat := range cfg.Copy {
		matches, err := filepath.Glob(filepath.Join(repoRoot, pat))
		if err != nil {
			continue
		}
		for _, src := range matches {
			fi, err := os.Stat(src)
			if err != nil || fi.IsDir() {
				continue
			}
			rel, err := filepath.Rel(repoRoot, src)
			if err != nil {
				continue
			}
			dst := filepath.Join(worktreePath, rel)
			if _, err := os.Stat(dst); err == nil {
				// Target already exists: skip copying
				continue
			}
			if err := copyFile(src, dst, fi.Mode()); err != nil {
				return fmt.Errorf("copy %s to %s: %w", src, dst, err)
			}
			if out != nil {
				_, _ = fmt.Fprintf(out, "copied %s\n", rel)
			}
		}
	}

	// 2. Dependencies
	if cfg.Deps == "copy" {
		candidates := []string{"node_modules", "vendor", ".venv"}
		for _, dir := range candidates {
			src := filepath.Join(repoRoot, dir)
			if fi, err := os.Stat(src); err == nil && fi.IsDir() {
				dst := filepath.Join(worktreePath, dir)
				if _, err := os.Stat(dst); err == nil {
					continue
				}
				if out != nil {
					_, _ = fmt.Fprintf(out, "copying %s...\n", dir)
				}
				if err := copyDirCoW(src, dst); err != nil {
					return fmt.Errorf("copy dir %s: %w", dir, err)
				}
			}
		}
	} else if cfg.Deps == "install" && strings.TrimSpace(cfg.InstallCmd) != "" {
		if out != nil {
			_, _ = fmt.Fprintf(out, "running install: %s\n", cfg.InstallCmd)
		}
		cmd := exec.Command("sh", "-c", cfg.InstallCmd)
		cmd.Dir = worktreePath
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install command %q: %w", cfg.InstallCmd, err)
		}
	}

	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDirCoW(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		cmd := exec.Command("cp", "-cR", src, dst)
		if err := cmd.Run(); err == nil {
			return nil
		}
	} else if runtime.GOOS == "linux" {
		cmd := exec.Command("cp", "-R", "--reflink=auto", src, dst)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback to standard recursive copy
	cmd := exec.Command("cp", "-R", src, dst)
	return cmd.Run()
}
