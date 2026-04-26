package model

import (
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestLayoutRoundTrip(t *testing.T) {
	original := Layout{
		Name:        "test-session",
		Description: "A test layout",
		Version:     1,
		SavedAt:     time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC),
		Workspaces: []Workspace{
			{
				Title:  "0 dev",
				CWD:    "/home/user/projects/myapp",
				Pinned: true,
				Index:  0,
				Panes: []Pane{
					{Type: "terminal", Focus: true},
					{Type: "terminal", Split: "right", Command: "go test ./..."},
				},
			},
			{
				Title:  "1 docs",
				CWD:    "/home/user/Documents",
				Pinned: false,
				Index:  1,
				Panes: []Pane{
					{Type: "terminal"},
				},
			},
		},
	}

	data, err := toml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Layout
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if len(decoded.Workspaces) != 2 {
		t.Fatalf("Workspaces = %d, want 2", len(decoded.Workspaces))
	}
	if decoded.Workspaces[0].CWD != "/home/user/projects/myapp" {
		t.Errorf("CWD = %q", decoded.Workspaces[0].CWD)
	}
	if len(decoded.Workspaces[0].Panes) != 2 {
		t.Fatalf("Panes = %d, want 2", len(decoded.Workspaces[0].Panes))
	}
	if decoded.Workspaces[0].Panes[1].Split != "right" {
		t.Errorf("Split = %q, want right", decoded.Workspaces[0].Panes[1].Split)
	}
	if decoded.Workspaces[0].Panes[1].Command != "go test ./..." {
		t.Errorf("Command = %q", decoded.Workspaces[0].Panes[1].Command)
	}
}

func TestLayoutTOMLFormat(t *testing.T) {
	layout := Layout{
		Name:    "minimal",
		Version: 1,
		SavedAt: time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC),
		Workspaces: []Workspace{
			{
				Title:  "0 main",
				CWD:    "/tmp",
				Pinned: true,
				Index:  0,
				Panes: []Pane{
					{Type: "terminal", Focus: true},
				},
			},
		},
	}

	data, err := toml.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	// go-toml/v2 uses single quotes for strings.
	if !strings.Contains(s, `name = 'minimal'`) {
		t.Errorf("missing name field in:\n%s", s)
	}
	if !strings.Contains(s, `title = '0 main'`) {
		t.Errorf("missing workspace title in:\n%s", s)
	}
	if !strings.Contains(s, `type = 'terminal'`) {
		t.Errorf("missing pane type in:\n%s", s)
	}
}

func TestLayoutRoundTripWithRemoteAndSurfaces(t *testing.T) {
	original := Layout{
		Name:    "remote-session",
		Version: 1,
		SavedAt: time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Workspaces: []Workspace{
			{
				Title:  "gpu-box",
				CWD:    "/home/dev/project",
				Pinned: true,
				Index:  0,
				Remote: &RemoteWorkspace{
					Enabled:         true,
					Provider:        "cmux_ssh",
					Destination:     "dev@gpu-box",
					Port:            2222,
					IdentityFile:    "~/.ssh/id_ed25519",
					SSHOptions:      []string{"StrictHostKeyChecking=accept-new"},
					HasIdentityFile: true,
					HasSSHOptions:   true,
					CaptureComplete: true,
				},
				Panes: []Pane{
					{
						Type:  "terminal",
						Focus: true,
						Surfaces: []Surface{
							{Type: "terminal", Command: "htop", Selected: true},
							{Type: "browser", URL: "http://localhost:3000", Index: 1},
						},
					},
				},
			},
		},
	}

	data, err := toml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Layout
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ws := decoded.Workspaces[0]
	if ws.Remote == nil {
		t.Fatal("remote metadata missing after round trip")
	}
	if ws.Remote.Destination != "dev@gpu-box" {
		t.Errorf("Destination = %q", ws.Remote.Destination)
	}
	if ws.Remote.IdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q", ws.Remote.IdentityFile)
	}
	if len(ws.Remote.SSHOptions) != 1 || ws.Remote.SSHOptions[0] != "StrictHostKeyChecking=accept-new" {
		t.Errorf("SSHOptions = %#v", ws.Remote.SSHOptions)
	}
	if len(ws.Panes[0].Surfaces) != 2 {
		t.Fatalf("Surfaces = %d, want 2", len(ws.Panes[0].Surfaces))
	}
	if ws.Panes[0].Surfaces[1].URL != "http://localhost:3000" {
		t.Errorf("browser URL = %q", ws.Panes[0].Surfaces[1].URL)
	}
}
