package activity

import (
	"crypto/sha256"
	"strings"
	"time"
)

type State string

const (
	StateIdle    State = "idle"
	StateWorking State = "working"
	StateBlocked State = "blocked"
	StateDone    State = "done"
	StateStopped State = "stopped"
)

type featureInfo struct {
	state        State
	lastHash     string
	lastChangeAt time.Time
	hookActive   bool
}

type Tracker struct {
	workingTimeout time.Duration
	clock          func() time.Time
	features       map[string]*featureInfo
}

func NewTracker(workingTimeout time.Duration, clock func() time.Time) *Tracker {
	if clock == nil {
		clock = time.Now
	}
	return &Tracker{
		workingTimeout: workingTimeout,
		clock:          clock,
		features:       make(map[string]*featureInfo),
	}
}

func (t *Tracker) get(feature string) *featureInfo {
	f, exists := t.features[feature]
	if !exists {
		f = &featureInfo{
			state:        StateIdle,
			lastChangeAt: t.clock(),
		}
		t.features[feature] = f
	}
	return f
}

func (t *Tracker) State(feature string) State {
	return t.get(feature).state
}

func (t *Tracker) MarkViewed(feature string) State {
	f := t.get(feature)
	if f.state == StateBlocked || f.state == StateDone {
		f.state = StateIdle
	}
	return f.state
}

func (t *Tracker) OnHook(feature, event string, isViewed bool) State {
	f := t.get(feature)
	f.hookActive = true

	switch event {
	case "working":
		f.state = StateWorking
		f.lastChangeAt = t.clock()
	case "blocked":
		if isViewed {
			f.state = StateIdle
		} else {
			f.state = StateBlocked
		}
	case "done":
		if isViewed {
			f.state = StateIdle
		} else {
			f.state = StateDone
		}
	}
	return f.state
}

func (t *Tracker) OnPoll(feature, screenText string, isDead, hasBell, isViewed bool) State {
	f := t.get(feature)
	now := t.clock()

	if isDead {
		f.state = StateStopped
		return f.state
	}

	if isViewed && (f.state == StateBlocked || f.state == StateDone) {
		f.state = StateIdle
	}

	if f.hookActive {
		if hasBell && !isViewed && f.state != StateWorking {
			f.state = StateBlocked
		}
		return f.state
	}

	// Screen-change guess fallback
	hash := hashScreen(screenText)
	if f.lastHash == "" {
		f.lastHash = hash
		f.lastChangeAt = now
		f.state = StateIdle
		return f.state
	}

	if hash != f.lastHash {
		f.lastHash = hash
		f.lastChangeAt = now
		f.state = StateWorking
		return f.state
	}

	// Hash matches (no change)
	if f.state == StateWorking {
		if now.Sub(f.lastChangeAt) >= t.workingTimeout {
			if hasBell && !isViewed {
				f.state = StateBlocked
			} else if isViewed {
				f.state = StateIdle
			} else {
				f.state = StateDone
			}
		}
	} else if hasBell && !isViewed {
		f.state = StateBlocked
	}

	return f.state
}

func hashScreen(text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for _, l := range lines {
		trimmed := strings.TrimRight(l, " \t\r")
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return string(sum[:])
}
