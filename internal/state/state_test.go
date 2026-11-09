package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateLoadSaveUpsertRemove(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	f := Feature{Name: "auth", Branch: "auth", WorktreePath: "/tmp/wt/auth", AgentID: "claude", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	st.Upsert(f)
	st.SelectedFeature = "auth"
	if err := Save(dir, st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load saved: %v", err)
	}
	if loaded.SelectedFeature != "auth" || len(loaded.Features) != 1 || loaded.Features[0].Name != "auth" {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}
	if _, found := loaded.Find("auth"); !found {
		t.Fatalf("expected to find auth feature")
	}
	loaded.Remove("auth")
	if len(loaded.Features) != 0 {
		t.Fatalf("expected empty features after Remove")
	}
	if _, found := loaded.Find("auth"); found {
		t.Fatalf("expected not to find auth feature after Remove")
	}
}

func TestAcquireLockAndStalePIDRecovery(t *testing.T) {
	dir := t.TempDir()
	// Write a stale lock with a dead PID (99999999).
	lockDir := filepath.Join(dir, "agenttree")
	_ = os.MkdirAll(lockDir, 0o755)
	_ = os.WriteFile(filepath.Join(lockDir, "lock"), []byte(`{"pid":99999999,"pane_id":"%99"}`), 0o644)

	rel, err := AcquireLock(dir, "%1")
	if err != nil {
		t.Fatalf("expected stale lock to be replaced, got: %v", err)
	}
	defer rel()

	// Second AcquireLock while current process is alive must fail with *LockHeldError.
	_, err = AcquireLock(dir, "%2")
	var held *LockHeldError
	if !errors.As(err, &held) || held.Holder.PaneID != "%1" {
		t.Fatalf("expected LockHeldError for pane %%1, got: %v", err)
	}
	if held.Error() == "" {
		t.Fatalf("expected non-empty error message")
	}
}
