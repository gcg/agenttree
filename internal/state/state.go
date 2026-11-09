package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Feature struct {
	Name         string    `json:"name"`
	Branch       string    `json:"branch"`
	WorktreePath string    `json:"worktree_path"`
	AgentID      string    `json:"agent_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type State struct {
	Features        []Feature `json:"features"`
	SelectedFeature string    `json:"selected_feature"`
}

type LockInfo struct {
	PID    int    `json:"pid"`
	PaneID string `json:"pane_id"`
}

type LockHeldError struct {
	Holder LockInfo
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("agenttree already running in pane %s (pid %d)", e.Holder.PaneID, e.Holder.PID)
}

func StatePath(gitCommonDir string) string {
	return filepath.Join(gitCommonDir, "agenttree", "state.json")
}

func lockPath(gitCommonDir string) string {
	return filepath.Join(gitCommonDir, "agenttree", "lock")
}

func Load(gitCommonDir string) (*State, error) {
	path := StatePath(gitCommonDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Features: []Feature{},
			}, nil
		}
		return nil, fmt.Errorf("read state file %q: %w", path, err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("unmarshal state file %q: %w", path, err)
	}
	if st.Features == nil {
		st.Features = []Feature{}
	}
	return &st, nil
}

func Save(gitCommonDir string, st *State) error {
	path := StatePath(gitCommonDir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("marshal state json: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write state to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp state to %q: %w", path, err)
	}

	return nil
}

func (s *State) Upsert(f Feature) {
	for i, existing := range s.Features {
		if existing.Name == f.Name {
			s.Features[i] = f
			return
		}
	}
	s.Features = append(s.Features, f)
}

func (s *State) Remove(name string) {
	filtered := make([]Feature, 0, len(s.Features))
	for _, f := range s.Features {
		if f.Name != name {
			filtered = append(filtered, f)
		}
	}
	s.Features = filtered
	if s.SelectedFeature == name {
		if len(s.Features) > 0 {
			s.SelectedFeature = s.Features[0].Name
		} else {
			s.SelectedFeature = ""
		}
	}
}

func (s *State) Find(name string) (Feature, bool) {
	for _, f := range s.Features {
		if f.Name == name {
			return f, true
		}
	}
	return Feature{}, false
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func AcquireLock(gitCommonDir, paneID string) (func() error, error) {
	lp := lockPath(gitCommonDir)
	dir := filepath.Dir(lp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory %q: %w", dir, err)
	}

	if data, err := os.ReadFile(lp); err == nil {
		var info LockInfo
		if err := json.Unmarshal(data, &info); err == nil {
			if isProcessAlive(info.PID) {
				return nil, &LockHeldError{Holder: info}
			}
		}
	}

	myInfo := LockInfo{
		PID:    os.Getpid(),
		PaneID: paneID,
	}
	data, err := json.Marshal(myInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal lock info: %w", err)
	}

	if err := os.WriteFile(lp, data, 0o644); err != nil {
		return nil, fmt.Errorf("write lock file %q: %w", lp, err)
	}

	release := func() error {
		curData, err := os.ReadFile(lp)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		var curInfo LockInfo
		if err := json.Unmarshal(curData, &curInfo); err == nil && curInfo.PID == myInfo.PID {
			return os.Remove(lp)
		}
		return nil
	}

	return release, nil
}
