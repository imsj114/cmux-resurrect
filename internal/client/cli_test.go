package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTreeResponseParsing(t *testing.T) {
	data, err := os.ReadFile("../../testdata/responses/tree-6-workspaces.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var resp TreeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Windows) != 1 {
		t.Fatalf("windows: got %d, want 1", len(resp.Windows))
	}

	win := resp.Windows[0]
	if win.WorkspaceCount != 6 {
		t.Errorf("workspace_count = %d, want 6", win.WorkspaceCount)
	}
	if len(win.Workspaces) != 3 {
		t.Errorf("workspaces in fixture: got %d, want 3", len(win.Workspaces))
	}

	// First workspace has 2 panes (split)
	ws0 := win.Workspaces[0]
	if ws0.Title != "0 api-server" {
		t.Errorf("ws0.Title = %q", ws0.Title)
	}
	if !ws0.Pinned {
		t.Error("ws0 should be pinned")
	}
	if len(ws0.Panes) != 2 {
		t.Errorf("ws0 panes: got %d, want 2", len(ws0.Panes))
	}

	// Surface type
	if ws0.Panes[0].Surfaces[0].Type != "terminal" {
		t.Errorf("surface type = %q", ws0.Panes[0].Surfaces[0].Type)
	}

	// Caller info
	if resp.Caller.WorkspaceRef != "workspace:6" {
		t.Errorf("caller workspace = %q", resp.Caller.WorkspaceRef)
	}
}

func TestCLIClientTreeRequestsIDs(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	binPath := filepath.Join(dir, "cmux-fake")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuoteForTest(argsPath) + "\nprintf '{\"windows\":[]}'\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cmux: %v", err)
	}

	client := &CLIClient{Binary: binPath, Timeout: 2 * time.Second}
	if _, err := client.Tree(); err != nil {
		t.Fatalf("Tree: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if got, want := string(args), "--id-format both tree --json\n"; got != want {
		t.Fatalf("cmux args = %q, want %q", got, want)
	}
}

func shellQuoteForTest(value string) string {
	out := "'"
	for _, r := range value {
		if r == '\'' {
			out += "'\\''"
			continue
		}
		out += string(r)
	}
	return out + "'"
}
