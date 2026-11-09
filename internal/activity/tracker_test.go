package activity

import (
	"testing"
	"time"
)

func TestTrackerHooksAndScreenFallback(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tr := NewTracker(3*time.Second, func() time.Time { return now })

	// Hook: working -> blocked -> viewed clears to idle.
	if s := tr.OnHook("feat-a", "working", false); s != StateWorking {
		t.Fatalf("expected working, got %s", s)
	}
	if s := tr.OnHook("feat-a", "blocked", false); s != StateBlocked {
		t.Fatalf("expected blocked, got %s", s)
	}
	if s := tr.MarkViewed("feat-a"); s != StateIdle {
		t.Fatalf("expected MarkViewed to clear blocked to idle, got %s", s)
	}

	// Screen-change fallback on feat-b (no hooks): initial poll establishes baseline (Idle).
	if s := tr.OnPoll("feat-b", "prompt >", false, false, false); s != StateIdle {
		t.Fatalf("expected initial poll idle, got %s", s)
	}
	// Output changes -> Working.
	now = now.Add(1 * time.Second)
	if s := tr.OnPoll("feat-b", "thinking...", false, false, false); s != StateWorking {
		t.Fatalf("expected working on text change, got %s", s)
	}
	// Quiet for >3s while not viewed -> Done (unread).
	now = now.Add(4 * time.Second)
	if s := tr.OnPoll("feat-b", "thinking...", false, false, false); s != StateDone {
		t.Fatalf("expected done after timeout while unviewed, got %s", s)
	}

	// Dead pane -> Stopped
	if s := tr.OnPoll("feat-b", "thinking...", true, false, false); s != StateStopped {
		t.Fatalf("expected stopped when dead, got %s", s)
	}
}
