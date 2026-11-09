package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func InstallWorktreeHooks(agentID, worktreePath, gitCommonDir, agenttreeBin string) error {
	if agentID != "claude" {
		return nil
	}

	claudeDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	settings := make(map[string]any)

	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok || hooksMap == nil {
		hooksMap = make(map[string]any)
	}

	hooksMap["UserPromptSubmit"] = agenttreeBin + " hook working"
	hooksMap["PreToolUse"] = agenttreeBin + " hook working"
	hooksMap["Notification"] = agenttreeBin + " hook blocked"
	hooksMap["Stop"] = agenttreeBin + " hook done"

	settings["hooks"] = hooksMap

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings.local.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}

	// Ensure .claude/settings.local.json is ignored via git info/exclude
	if gitCommonDir != "" {
		excludePath := filepath.Join(gitCommonDir, "info", "exclude")
		_ = os.MkdirAll(filepath.Dir(excludePath), 0o755)
		excludeData, _ := os.ReadFile(excludePath)
		if !strings.Contains(string(excludeData), ".claude/settings.local.json") {
			f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.WriteString("\n.claude/settings.local.json\n")
				_ = f.Close()
			}
		}
	}

	return nil
}

func WrapAgentCommand(agentID, baseCmd, agenttreeBin string) string {
	if agentID == "codex" {
		notifyArg := fmt.Sprintf(`-c 'notify=["%s","hook","done"]'`, agenttreeBin)
		parts := strings.Fields(baseCmd)
		if len(parts) > 0 && parts[0] == "codex" {
			return parts[0] + " " + notifyArg + " " + strings.Join(parts[1:], " ")
		}
		return baseCmd + " " + notifyArg
	}
	return baseCmd
}
