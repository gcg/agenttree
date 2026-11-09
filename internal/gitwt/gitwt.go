package gitwt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type WorktreeInfo struct {
	Path   string
	Branch string
}

type SafetyReport struct {
	Dirty    bool
	Unpushed bool
	Unmerged bool
}

func (r SafetyReport) NeedsForce() bool {
	return r.Dirty || r.Unpushed || r.Unmerged
}

var (
	nonSlugChars = regexp.MustCompile(`[^a-z0-9._-]+`)
	multiHyphens = regexp.MustCompile(`-+`)
)

func Slugify(name, branchPrefix string) (string, string, error) {
	lower := strings.ToLower(strings.TrimSpace(name))
	slug := nonSlugChars.ReplaceAllString(lower, "-")
	slug = multiHyphens.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-.")
	if slug == "" {
		return "", "", fmt.Errorf("empty feature slug derived from %q", name)
	}

	branch := branchPrefix + slug
	cmd := exec.Command("git", "check-ref-format", "--branch", branch)
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("invalid branch ref %q: %w", branch, err)
	}

	return slug, branch, nil
}

func ResolveRepo(cwd string) (string, string, error) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git rev-parse --git-common-dir: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	gitCommonDir := filepath.Clean(strings.TrimSpace(string(out)))
	repoRoot := filepath.Dir(gitCommonDir)
	return repoRoot, gitCommonDir, nil
}

func DefaultBranch(repoRoot string) string {
	cmd := exec.Command("git", "-C", repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD")
	if out, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			short := strings.TrimPrefix(ref, "refs/remotes/origin/")
			fetchCmd := exec.Command("git", "-C", repoRoot, "fetch", "origin", short)
			_ = fetchCmd.Run()
			return "origin/" + short
		}
	}

	for _, candidate := range []string{"main", "master"} {
		check := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+candidate)
		if check.Run() == nil {
			return candidate
		}
	}

	return "HEAD"
}

func EnsureIgnored(repoRoot, gitCommonDir, worktreeDir, mode string) (string, error) {
	checkCmd := exec.Command("git", "-C", repoRoot, "check-ignore", "-q", worktreeDir)
	if checkCmd.Run() == nil {
		return mode, nil
	}

	hasMergeHead := fileExists(filepath.Join(gitCommonDir, "MERGE_HEAD"))
	hasRebaseMerge := fileExists(filepath.Join(gitCommonDir, "rebase-merge"))
	hasRebaseApply := fileExists(filepath.Join(gitCommonDir, "rebase-apply"))
	if (hasMergeHead || hasRebaseMerge || hasRebaseApply) && mode == "gitignore-commit" {
		mode = "gitignore"
	}

	cleanDir := strings.Trim(worktreeDir, "/")
	line := "/" + cleanDir + "/\n"

	if mode == "exclude" {
		exclPath := filepath.Join(gitCommonDir, "info", "exclude")
		if err := os.MkdirAll(filepath.Dir(exclPath), 0o755); err != nil {
			return mode, err
		}
		if err := appendToFile(exclPath, line); err != nil {
			return mode, err
		}
		return "exclude", nil
	}

	giPath := filepath.Join(repoRoot, ".gitignore")
	if err := appendToFile(giPath, line); err != nil {
		return mode, err
	}

	if mode == "gitignore-commit" {
		commitCmd := exec.Command("git", "-C", repoRoot, "commit", "--only", ".gitignore", "-m", "chore: ignore "+cleanDir+" (agenttree)")
		if err := commitCmd.Run(); err != nil {
			return "gitignore", nil
		}
		return "gitignore-commit", nil
	}

	return "gitignore", nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

func AddWorktree(repoRoot, worktreePath, branch, baseRef string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	checkBranch := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	branchExists := checkBranch.Run() == nil

	var cmd *exec.Cmd
	if branchExists {
		cmd = exec.Command("git", "-C", repoRoot, "worktree", "add", worktreePath, branch)
	} else {
		cmd = exec.Command("git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreePath, baseRef)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ListWorktrees(repoRoot, worktreesRoot string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	cleanRoot, err := filepath.EvalSymlinks(worktreesRoot)
	if err != nil {
		cleanRoot = filepath.Clean(worktreesRoot)
	}
	var result []WorktreeInfo

	blocks := bytes.Split(out, []byte("\n\n"))
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(string(block)), "\n")
		var curPath, curBranch string
		for _, line := range lines {
			if strings.HasPrefix(line, "worktree ") {
				curPath = filepath.Clean(strings.TrimPrefix(line, "worktree "))
			} else if strings.HasPrefix(line, "branch ") {
				ref := strings.TrimPrefix(line, "branch ")
				curBranch = strings.TrimPrefix(ref, "refs/heads/")
			}
		}
		if curPath != "" {
			evalCurPath, err := filepath.EvalSymlinks(curPath)
			if err != nil {
				evalCurPath = curPath
			}
			rel, err := filepath.Rel(cleanRoot, evalCurPath)
			if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				result = append(result, WorktreeInfo{
					Path:   curPath,
					Branch: curBranch,
				})
			}
		}
	}

	return result, nil
}

func CheckSafety(repoRoot, worktreePath, branch string, prMerged bool) (SafetyReport, error) {
	var report SafetyReport

	// Check dirty
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	if out, err := statusCmd.Output(); err == nil {
		if len(bytes.TrimSpace(out)) > 0 {
			report.Dirty = true
		}
	} else {
		return report, fmt.Errorf("git status --porcelain: %w", err)
	}

	if prMerged {
		return report, nil
	}

	// Check unmerged commits relative to HEAD
	ancCmd := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", branch, "HEAD")
	if err := ancCmd.Run(); err != nil {
		report.Unmerged = true
	}

	// Check unpushed commits relative to upstream
	upstreamCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "@{u}")
	if upstreamCmd.Run() == nil {
		revCountCmd := exec.Command("git", "-C", worktreePath, "rev-list", "@{u}..HEAD", "--count")
		if out, err := revCountCmd.Output(); err == nil {
			count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
			if count > 0 {
				report.Unpushed = true
			}
		}
	}

	return report, nil
}

func RemoveWorktree(repoRoot, worktreePath, branch string, force bool) error {
	args := []string{"-C", repoRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	rmCmd := exec.Command("git", args...)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	branchFlag := "-d"
	if force {
		branchFlag = "-D"
	}
	brCmd := exec.Command("git", "-C", repoRoot, "branch", branchFlag, branch)
	_ = brCmd.Run()

	return nil
}
