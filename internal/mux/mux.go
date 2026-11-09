package mux

type PaneID string

type Multiplexer interface {
	EnsureSession(name string) error
	NewAgentPane(session, feature, cwd, cmd string, env map[string]string) (PaneID, error)
	FindPanes(session string) (map[string]PaneID, error)
	Show(feature PaneID, slot PaneID) error
	Capture(p PaneID) (string, error)
	HasBell(p PaneID) (bool, error)
	IsDead(p PaneID) (bool, error)
	Respawn(p PaneID, cmd string) error
	Kill(p PaneID) error
	Popup(cwd, cmd string, wPct, hPct int) error
	BindKey(table, key, cmd string) (skipped bool, err error)
	UnbindKey(table, key string) error
}
