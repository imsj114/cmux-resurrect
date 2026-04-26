package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drolosoft/cmux-resurrect/internal/client"
	"github.com/drolosoft/cmux-resurrect/internal/model"
	"github.com/drolosoft/cmux-resurrect/internal/persist"
)

// mockClient implements client.Backend for testing.
type mockClient struct {
	treeResp     *client.TreeResponse
	sidebarCWDs  map[string]string
	pingErr      error
	workspaceSeq int
}

type mockCmuxClient struct {
	mockClient
	workspaceList     *client.WorkspaceListResponse
	paneLists         map[string]*client.PaneListResponse
	terminalList      *client.TerminalListResponse
	remoteStatuses    []client.RemoteStatusPayload
	remoteStatusCalls []string
	remoteWorkspaceID string
	remoteIDSet       bool
	remoteCreated     []client.RemoteSSHOpts
	panesCreated      []client.PaneCreateOpts
	surfacesCreated   []client.PaneCreateOpts
}

func (m *mockClient) Ping() error { return m.pingErr }

func (m *mockClient) Tree() (*client.TreeResponse, error) {
	return m.treeResp, nil
}

func (m *mockClient) SidebarState(ref string) (*client.SidebarState, error) {
	cwd, ok := m.sidebarCWDs[ref]
	if !ok {
		cwd = "/tmp/unknown"
	}
	return &client.SidebarState{CWD: cwd, FocusedCWD: cwd}, nil
}

func (m *mockClient) ListWorkspaces() ([]client.WorkspaceInfo, error) {
	return nil, nil
}

func (m *mockClient) NewWorkspace(opts client.NewWorkspaceOpts) (string, error) {
	m.workspaceSeq++
	return "workspace:new", nil
}

func (m *mockClient) RenameWorkspace(ref, title string) error  { return nil }
func (m *mockClient) SelectWorkspace(ref string) error         { return nil }
func (m *mockClient) NewSplit(dir, ref string) (string, error) { return "surface:mock", nil }
func (m *mockClient) FocusPane(pane, ws string) error          { return nil }
func (m *mockClient) Send(ws, surf, text string) error         { return nil }
func (m *mockClient) PinWorkspace(ref string) error            { return nil }
func (m *mockClient) CloseWorkspace(ref string) error          { return nil }
func (m *mockClient) DryRunFormatter() client.DryRunFormatter  { return client.CmuxDryRun{} }

func (m *mockCmuxClient) WorkspaceListJSON() (*client.WorkspaceListResponse, error) {
	return m.workspaceList, nil
}

func (m *mockCmuxClient) PaneListJSON(workspaceRef string) (*client.PaneListResponse, error) {
	if m.paneLists == nil {
		return nil, nil
	}
	return m.paneLists[workspaceRef], nil
}

func (m *mockCmuxClient) TerminalListJSON() (*client.TerminalListResponse, error) {
	return m.terminalList, nil
}

func (m *mockCmuxClient) RemoteStatus(workspaceID string) (*client.RemoteStatusPayload, error) {
	m.remoteStatusCalls = append(m.remoteStatusCalls, workspaceID)
	if len(m.remoteStatuses) > 0 {
		index := len(m.remoteStatusCalls) - 1
		if index >= len(m.remoteStatuses) {
			index = len(m.remoteStatuses) - 1
		}
		status := m.remoteStatuses[index]
		return &status, nil
	}
	return &client.RemoteStatusPayload{
		Enabled: true,
		State:   "connected",
		Proxy:   client.RemoteProxyPayload{State: "ready"},
	}, nil
}

func (m *mockCmuxClient) NewRemoteWorkspace(opts client.RemoteSSHOpts) (string, string, error) {
	m.remoteCreated = append(m.remoteCreated, opts)
	workspaceID := m.remoteWorkspaceID
	if !m.remoteIDSet && workspaceID == "" {
		workspaceID = "remote-new-id"
	}
	return "workspace:remote-new", workspaceID, nil
}

func (m *mockCmuxClient) NewPane(opts client.PaneCreateOpts) (string, string, error) {
	m.panesCreated = append(m.panesCreated, opts)
	return "surface:browser-new", "pane:browser-new", nil
}

func (m *mockCmuxClient) NewSurface(opts client.PaneCreateOpts) (string, error) {
	m.surfacesCreated = append(m.surfacesCreated, opts)
	return "surface:extra-new", nil
}

func TestSave_FromFixture(t *testing.T) {
	// Load tree fixture.
	data, err := os.ReadFile("../../testdata/responses/tree-6-workspaces.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var treeResp client.TreeResponse
	if err := json.Unmarshal(data, &treeResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	mc := &mockClient{
		treeResp: &treeResp,
		sidebarCWDs: map[string]string{
			"workspace:1": "/home/user/projects/api-server",
			"workspace:2": "/home/user/Documents/notes",
			"workspace:3": "/home/user/projects/webapp",
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("test-session", "unit test")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if layout.Name != "test-session" {
		t.Errorf("Name = %q", layout.Name)
	}
	if len(layout.Workspaces) != 3 {
		t.Fatalf("Workspaces = %d, want 3", len(layout.Workspaces))
	}

	// First workspace should have 2 panes (it has 2 in the fixture).
	ws0 := layout.Workspaces[0]
	if ws0.Title != "0 api-server" {
		t.Errorf("ws0.Title = %q", ws0.Title)
	}
	if ws0.CWD != "/home/user/projects/api-server" {
		t.Errorf("ws0.CWD = %q", ws0.CWD)
	}
	if len(ws0.Panes) != 2 {
		t.Errorf("ws0.Panes = %d, want 2", len(ws0.Panes))
	}
	// Second pane should default to split "right".
	if ws0.Panes[1].Split != "right" {
		t.Errorf("ws0.Panes[1].Split = %q, want right", ws0.Panes[1].Split)
	}

	// Verify file was written.
	if !store.Exists("test-session") {
		t.Error("layout file not written")
	}
}

func TestSave_MergePreservesUserEdits(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/responses/tree-6-workspaces.json")
	var treeResp client.TreeResponse
	_ = json.Unmarshal(data, &treeResp)

	mc := &mockClient{
		treeResp: &treeResp,
		sidebarCWDs: map[string]string{
			"workspace:1": "/home/user/projects/api-server",
			"workspace:2": "/home/user/Documents/notes",
			"workspace:3": "/home/user/projects/webapp",
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	// First save.
	_, _ = saver.Save("merge-test", "")

	// Manually edit the saved file to add user customizations.
	layout, _ := store.Load("merge-test")
	if len(layout.Workspaces[0].Panes) > 1 {
		layout.Workspaces[0].Panes[1].Split = "down"
		layout.Workspaces[0].Panes[1].Command = "make watch"
	}
	layout.Description = "my custom description"
	_ = store.Save("merge-test", layout)

	// Second save should preserve user edits.
	layout2, err := saver.Save("merge-test", "")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	if layout2.Description != "my custom description" {
		t.Errorf("Description = %q, want 'my custom description'", layout2.Description)
	}
	if len(layout2.Workspaces[0].Panes) > 1 {
		if layout2.Workspaces[0].Panes[1].Split != "down" {
			t.Errorf("Split = %q, want down (user edit)", layout2.Workspaces[0].Panes[1].Split)
		}
		if layout2.Workspaces[0].Panes[1].Command != "make watch" {
			t.Errorf("Command = %q, want 'make watch' (user edit)", layout2.Workspaces[0].Panes[1].Command)
		}
		if got := layout2.Workspaces[0].Panes[1].Surfaces[0].Command; got != "make watch" {
			t.Errorf("Surface command = %q, want 'make watch' (legacy user edit mirrored)", got)
		}
	}
}

func TestSave_CapturesPaneTopologyFromGeometry(t *testing.T) {
	treeResp := paneTopologyTree()
	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/tmp/project"},
		},
		paneLists: map[string]*client.PaneListResponse{
			"workspace:1": {
				WorkspaceRef: "workspace:1",
				Panes: []client.PaneRow{
					{Ref: "pane:0", Index: 0, PixelFrame: client.RectFrame{X: 0, Y: 0, Width: 1000, Height: 600}},
					{Ref: "pane:1", Index: 1, PixelFrame: client.RectFrame{X: 0, Y: 600, Width: 500, Height: 400}},
					{Ref: "pane:2", Index: 2, PixelFrame: client.RectFrame{X: 500, Y: 600, Width: 500, Height: 400}},
				},
			},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("pane-topology", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	panes := layout.Workspaces[0].Panes
	if got := paneIndexes(panes); got != "0,1,2" {
		t.Fatalf("pane indexes = %s, want 0,1,2", got)
	}
	if panes[1].Split != "down" || panes[1].FocusTarget != 0 {
		t.Errorf("pane 1 split/focus = %q/%d, want down/0", panes[1].Split, panes[1].FocusTarget)
	}
	if panes[2].Split != "right" || panes[2].FocusTarget != 1 {
		t.Errorf("pane 2 split/focus = %q/%d, want right/1", panes[2].Split, panes[2].FocusTarget)
	}
}

func TestSave_CapturesPaneTopologyCreationOrder(t *testing.T) {
	treeResp := paneTopologyTree()
	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/tmp/project"},
		},
		paneLists: map[string]*client.PaneListResponse{
			"workspace:1": {
				WorkspaceRef: "workspace:1",
				Panes: []client.PaneRow{
					{Ref: "pane:0", Index: 0, PixelFrame: client.RectFrame{X: 0, Y: 0, Width: 500, Height: 500}},
					{Ref: "pane:1", Index: 1, PixelFrame: client.RectFrame{X: 0, Y: 500, Width: 500, Height: 500}},
					{Ref: "pane:2", Index: 2, PixelFrame: client.RectFrame{X: 500, Y: 0, Width: 500, Height: 1000}},
				},
			},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("pane-creation-order", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	panes := layout.Workspaces[0].Panes
	if got := paneIndexes(panes); got != "0,2,1" {
		t.Fatalf("pane indexes = %s, want 0,2,1", got)
	}
	if panes[1].Split != "right" || panes[1].FocusTarget != 0 {
		t.Errorf("pane 2 split/focus = %q/%d, want right/0", panes[1].Split, panes[1].FocusTarget)
	}
	if panes[2].Split != "down" || panes[2].FocusTarget != 0 {
		t.Errorf("pane 1 split/focus = %q/%d, want down/0", panes[2].Split, panes[2].FocusTarget)
	}
}

func TestSave_CapturesTerminalSurfaceWorkingDirectories(t *testing.T) {
	treeResp := &client.TreeResponse{
		Windows: []client.TreeWindow{
			{
				Workspaces: []client.TreeWorkspace{
					{
						Ref:   "workspace:1",
						Title: "cwd-layout",
						Panes: []client.TreePane{
							{
								Ref:                "pane:0",
								Index:              0,
								SelectedSurfaceRef: "surface:0",
								Surfaces: []client.TreeSurface{
									{ID: "surface-uuid-0", Ref: "surface:0", Type: "terminal", IndexInPane: 0, SelectedInPane: true},
								},
							},
							{
								Ref:                "pane:1",
								Index:              1,
								SelectedSurfaceRef: "surface:1",
								Surfaces: []client.TreeSurface{
									{ID: "surface-uuid-1", Ref: "surface:1", Type: "terminal", IndexInPane: 0, SelectedInPane: true},
								},
							},
						},
					},
				},
			},
		},
	}
	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/tmp/project"},
		},
		terminalList: &client.TerminalListResponse{
			Terminals: []client.TerminalRow{
				{SurfaceID: "surface-uuid-0", SurfaceRef: "surface:0", CurrentDirectory: "/tmp/project"},
				{SurfaceID: "surface-uuid-1", SurfaceRef: "surface:1", CurrentDirectory: "/tmp/other package"},
			},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("cwd-layout", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	panes := layout.Workspaces[0].Panes
	if got := panes[0].CWD; got != "/tmp/project" {
		t.Errorf("pane 0 CWD = %q, want /tmp/project", got)
	}
	if got := panes[0].Surfaces[0].CWD; got != "/tmp/project" {
		t.Errorf("surface 0 CWD = %q, want /tmp/project", got)
	}
	if got := panes[1].CWD; got != "/tmp/other package" {
		t.Errorf("pane 1 CWD = %q, want /tmp/other package", got)
	}
	if got := panes[1].Surfaces[0].CWD; got != "/tmp/other package" {
		t.Errorf("surface 1 CWD = %q, want /tmp/other package", got)
	}
}

func TestSave_CapturesRemoteTerminalWorkingDirectoriesFromTitles(t *testing.T) {
	treeResp := &client.TreeResponse{
		Windows: []client.TreeWindow{
			{
				Workspaces: []client.TreeWorkspace{
					{
						ID:    "workspace-uuid-1",
						Ref:   "workspace:1",
						Title: "remote-layout",
						Panes: []client.TreePane{
							{
								Ref:                "pane:0",
								Index:              0,
								SelectedSurfaceRef: "surface:0",
								Surfaces: []client.TreeSurface{
									{ID: "surface-uuid-0", Ref: "surface:0", Type: "terminal", Title: "ubuntu@ubuntu-server: ~/bin", IndexInPane: 0, SelectedInPane: true},
								},
							},
						},
					},
				},
			},
		},
	}
	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/Users/local/project"},
		},
		workspaceList: &client.WorkspaceListResponse{
			Workspaces: []client.WorkspaceRow{
				{
					ID:    "workspace-uuid-1",
					Ref:   "workspace:1",
					Title: "remote-layout",
					Remote: client.RemoteStatusPayload{
						Enabled:     true,
						Destination: "home",
					},
				},
			},
		},
		terminalList: &client.TerminalListResponse{
			Terminals: []client.TerminalRow{
				{SurfaceID: "surface-uuid-0", SurfaceRef: "surface:0", CurrentDirectory: "/Users/local/project"},
			},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("remote-cwd-layout", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	surface := layout.Workspaces[0].Panes[0].Surfaces[0]
	if surface.CWD != "~/bin" {
		t.Errorf("remote surface CWD = %q, want ~/bin from title", surface.CWD)
	}
}

func TestSave_CapturesCodexAgentSession(t *testing.T) {
	hookDir := t.TempDir()
	t.Setenv("CMUX_AGENT_HOOK_STATE_DIR", hookDir)
	hookState := `{
		"sessions": {
			"codex-session-1": {
				"sessionId": "codex-session-1",
				"workspaceId": "workspace-uuid-1",
				"surfaceId": "surface-uuid-1",
				"cwd": "/tmp/codex repo",
				"updatedAt": 100
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(hookDir, "codex-hook-sessions.json"), []byte(hookState), 0o600); err != nil {
		t.Fatalf("write hook state: %v", err)
	}

	treeResp := &client.TreeResponse{
		Windows: []client.TreeWindow{
			{
				Workspaces: []client.TreeWorkspace{
					{
						Ref:   "workspace:1",
						Title: "codex-layout",
						Panes: []client.TreePane{
							{
								Ref:                "pane:0",
								Index:              0,
								SelectedSurfaceRef: "surface:1",
								Surfaces: []client.TreeSurface{
									{ID: "surface-uuid-1", Ref: "surface:1", Type: "terminal", IndexInPane: 0, SelectedInPane: true},
								},
							},
						},
					},
				},
			},
		},
	}
	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/tmp/codex repo"},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("codex-layout", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	surface := layout.Workspaces[0].Panes[0].Surfaces[0]
	if surface.CWD != "/tmp/codex repo" {
		t.Errorf("surface CWD = %q, want hook cwd", surface.CWD)
	}
	if surface.Agent == nil {
		t.Fatal("missing agent session")
	}
	if surface.Agent.Kind != "codex" || surface.Agent.SessionID != "codex-session-1" {
		t.Errorf("agent = %#v, want codex/codex-session-1", surface.Agent)
	}
}

// TestSave_PreservesWorkspaceDescription verifies that a user-edited
// per-workspace description survives a re-save. cmux itself doesn't
// expose descriptions through Tree/SidebarState, so crex keeps them as
// a user-annotated field (aligned with cmux v0.63.2's "editable
// workspace descriptions" feature).
func TestSave_PreservesWorkspaceDescription(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/responses/tree-6-workspaces.json")
	var treeResp client.TreeResponse
	_ = json.Unmarshal(data, &treeResp)

	mc := &mockClient{
		treeResp: &treeResp,
		sidebarCWDs: map[string]string{
			"workspace:1": "/home/user/projects/api-server",
			"workspace:2": "/home/user/Documents/notes",
			"workspace:3": "/home/user/projects/webapp",
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	// First save.
	if _, err := saver.Save("desc-test", ""); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Annotate workspace[0] with a description.
	layout, _ := store.Load("desc-test")
	layout.Workspaces[0].Description = "backend API — reads postgres"
	if err := store.Save("desc-test", layout); err != nil {
		t.Fatalf("annotated save: %v", err)
	}

	// Re-save — the live tree doesn't expose descriptions, so the
	// merge must preserve the annotation.
	layout2, err := saver.Save("desc-test", "")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	if got := layout2.Workspaces[0].Description; got != "backend API — reads postgres" {
		t.Errorf("Workspaces[0].Description = %q, want preserved annotation", got)
	}
	// Other workspaces should remain empty (no bleed).
	for i := 1; i < len(layout2.Workspaces); i++ {
		if got := layout2.Workspaces[i].Description; got != "" {
			t.Errorf("Workspaces[%d].Description = %q, want empty", i, got)
		}
	}
}

func paneTopologyTree() *client.TreeResponse {
	return &client.TreeResponse{
		Windows: []client.TreeWindow{
			{
				Workspaces: []client.TreeWorkspace{
					{
						Ref:   "workspace:1",
						Title: "layout",
						Panes: []client.TreePane{
							{Ref: "pane:0", Index: 0, Surfaces: []client.TreeSurface{{Ref: "surface:0", Type: "terminal", IndexInPane: 0, SelectedInPane: true}}},
							{Ref: "pane:1", Index: 1, Surfaces: []client.TreeSurface{{Ref: "surface:1", Type: "terminal", IndexInPane: 0, SelectedInPane: true}}},
							{Ref: "pane:2", Index: 2, Surfaces: []client.TreeSurface{{Ref: "surface:2", Type: "terminal", IndexInPane: 0, SelectedInPane: true}}},
						},
					},
				},
			},
		},
	}
}

func paneIndexes(panes []model.Pane) string {
	parts := make([]string, 0, len(panes))
	for _, pane := range panes {
		parts = append(parts, fmt.Sprintf("%d", pane.Index))
	}
	return strings.Join(parts, ",")
}

func TestSave_CapturesRemoteMetadataAndSurfaces(t *testing.T) {
	port := 2222
	url := "http://localhost:3000"
	treeResp := &client.TreeResponse{
		Windows: []client.TreeWindow{
			{
				Workspaces: []client.TreeWorkspace{
					{
						ID:     "workspace-id-1",
						Ref:    "workspace:1",
						Title:  "gpu-box",
						Index:  0,
						Pinned: true,
						Panes: []client.TreePane{
							{
								Ref:                "pane:1",
								Index:              0,
								Focused:            true,
								SelectedSurfaceRef: "surface:1",
								Surfaces: []client.TreeSurface{
									{Ref: "surface:1", Type: "terminal", Title: "ssh", Index: 0, IndexInPane: 0, SelectedInPane: true},
									{Ref: "surface:2", Type: "browser", Title: "app", URL: &url, Index: 1, IndexInPane: 1},
								},
							},
						},
					},
				},
			},
		},
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    treeResp,
			sidebarCWDs: map[string]string{"workspace:1": "/tmp/local"},
		},
		workspaceList: &client.WorkspaceListResponse{
			Workspaces: []client.WorkspaceRow{
				{
					ID:               "workspace-id-1",
					Ref:              "workspace:1",
					Title:            "gpu-box",
					CurrentDirectory: "/home/dev/project",
					Remote: client.RemoteStatusPayload{
						Enabled:         true,
						Destination:     "dev@gpu-box",
						Port:            &port,
						HasIdentityFile: true,
						HasSSHOptions:   true,
					},
				},
			},
		},
	}

	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	saver := &Saver{Client: mc, Store: store}

	layout, err := saver.Save("remote", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	ws := layout.Workspaces[0]
	if ws.CWD != "/home/dev/project" {
		t.Errorf("CWD = %q, want workspace.list current_directory", ws.CWD)
	}
	if ws.Remote == nil {
		t.Fatal("Remote metadata missing")
	}
	if ws.Remote.Destination != "dev@gpu-box" || ws.Remote.Port != 2222 {
		t.Errorf("remote target = %q:%d", ws.Remote.Destination, ws.Remote.Port)
	}
	if ws.Remote.CaptureComplete {
		t.Error("CaptureComplete should be false without hidden replay fields")
	}
	if len(ws.Panes[0].Surfaces) != 2 {
		t.Fatalf("Surfaces = %d, want 2", len(ws.Panes[0].Surfaces))
	}
	if ws.Panes[0].Type != "terminal" {
		t.Errorf("legacy pane type = %q, want selected terminal", ws.Panes[0].Type)
	}
	if ws.Panes[0].Surfaces[1].URL != "http://localhost:3000" {
		t.Errorf("browser URL = %q", ws.Panes[0].Surfaces[1].URL)
	}

	saved, _ := store.Load("remote")
	saved.Workspaces[0].Remote.IdentityFile = "~/.ssh/id_ed25519"
	saved.Workspaces[0].Remote.SSHOptions = []string{"StrictHostKeyChecking=accept-new"}
	if err := store.Save("remote", saved); err != nil {
		t.Fatalf("save edited layout: %v", err)
	}

	layout2, err := saver.Save("remote", "")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	remote := layout2.Workspaces[0].Remote
	if remote.IdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q", remote.IdentityFile)
	}
	if len(remote.SSHOptions) != 1 || remote.SSHOptions[0] != "StrictHostKeyChecking=accept-new" {
		t.Errorf("SSHOptions = %#v", remote.SSHOptions)
	}
	if !remote.CaptureComplete {
		t.Errorf("CaptureComplete should be true after replay fields are preserved: %q", remote.Warning)
	}
}
