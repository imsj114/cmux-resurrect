package model

import "time"

// Layout represents a complete terminal session layout.
type Layout struct {
	Name        string      `toml:"name"`
	Description string      `toml:"description,omitempty"`
	Version     int         `toml:"version"`
	SavedAt     time.Time   `toml:"saved_at"`
	Workspaces  []Workspace `toml:"workspace"`
}

// Workspace represents a single cmux workspace (tab).
type Workspace struct {
	Title       string           `toml:"title"`
	Description string           `toml:"description,omitempty"`
	CWD         string           `toml:"cwd"`
	Pinned      bool             `toml:"pinned"`
	Index       int              `toml:"index"`
	Active      bool             `toml:"active,omitempty"`
	Remote      *RemoteWorkspace `toml:"remote,omitempty"`
	Panes       []Pane           `toml:"pane"`
}

// RemoteWorkspace stores replay metadata for a remote workspace.
type RemoteWorkspace struct {
	Enabled         bool     `toml:"enabled"`
	Provider        string   `toml:"provider,omitempty"`
	Destination     string   `toml:"destination,omitempty"`
	Port            int      `toml:"port,omitempty"`
	IdentityFile    string   `toml:"identity_file,omitempty"`
	SSHOptions      []string `toml:"ssh_option,omitempty"`
	HasIdentityFile bool     `toml:"has_identity_file,omitempty"`
	HasSSHOptions   bool     `toml:"has_ssh_options,omitempty"`
	CaptureComplete bool     `toml:"capture_complete"`
	Warning         string   `toml:"warning,omitempty"`
}

// Pane represents a terminal or browser pane within a workspace.
type Pane struct {
	Type        string    `toml:"type"`
	Split       string    `toml:"split,omitempty"`
	CWD         string    `toml:"cwd,omitempty"`
	Command     string    `toml:"command,omitempty"`
	Focus       bool      `toml:"focus,omitempty"`
	URL         string    `toml:"url,omitempty"`
	Index       int       `toml:"index,omitempty"`
	FocusTarget int       `toml:"focus_target,omitempty"`
	Surfaces    []Surface `toml:"surface,omitempty"`
}

// Surface represents a terminal or browser surface inside a pane.
type Surface struct {
	Type     string `toml:"type"`
	Title    string `toml:"title,omitempty"`
	URL      string `toml:"url,omitempty"`
	Command  string `toml:"command,omitempty"`
	Index    int    `toml:"index,omitempty"`
	Selected bool   `toml:"selected,omitempty"`
}

// LayoutMeta holds summary info about a saved layout (for list command).
type LayoutMeta struct {
	Name           string
	Description    string
	SavedAt        time.Time
	WorkspaceCount int
	FilePath       string
}
