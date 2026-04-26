package orchestrate

import (
	"testing"
	"time"

	"github.com/drolosoft/cmux-resurrect/internal/client"
	"github.com/drolosoft/cmux-resurrect/internal/model"
	"github.com/drolosoft/cmux-resurrect/internal/persist"
)

func TestRestore_DryRun(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)

	layout := &model.Layout{
		Name:    "dry-test",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title:  "0 dev",
				CWD:    "/tmp/project",
				Pinned: true,
				Index:  0,
				Active: true,
				Panes: []model.Pane{
					{Type: "terminal", Focus: true},
					{Type: "terminal", Split: "right", Command: "go test ./..."},
				},
			},
			{
				Title:  "1 docs",
				CWD:    "/tmp/docs",
				Pinned: false,
				Index:  1,
				Panes: []model.Pane{
					{Type: "terminal", Command: "claude"},
				},
			},
		},
	}
	_ = store.Save("dry-test", layout)

	mc := &mockClient{sidebarCWDs: map[string]string{}}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("dry-test", true, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore dry-run: %v", err)
	}

	if !result.DryRun {
		t.Error("DryRun should be true")
	}
	if result.WorkspacesTotal != 2 {
		t.Errorf("WorkspacesTotal = %d, want 2", result.WorkspacesTotal)
	}
	if result.WorkspacesOK != 2 {
		t.Errorf("WorkspacesOK = %d, want 2", result.WorkspacesOK)
	}
	if len(result.Commands) == 0 {
		t.Error("expected dry-run commands")
	}

	// Verify expected commands.
	hasNewWorkspace := false
	hasRename := false
	hasSplit := false
	hasSend := false
	hasSelect := false
	for _, cmd := range result.Commands {
		if containsStr(cmd, "new-workspace") {
			hasNewWorkspace = true
		}
		if containsStr(cmd, "rename-workspace") {
			hasRename = true
		}
		if containsStr(cmd, "new-split") {
			hasSplit = true
		}
		if containsStr(cmd, "send") {
			hasSend = true
		}
		if containsStr(cmd, "select-workspace") {
			hasSelect = true
		}
	}
	if !hasNewWorkspace {
		t.Error("missing new-workspace command")
	}
	if !hasRename {
		t.Error("missing rename-workspace command")
	}
	if !hasSplit {
		t.Error("missing new-split command")
	}
	if !hasSend {
		t.Error("missing send command")
	}
	if !hasSelect {
		t.Error("missing select-workspace command")
	}
}

func TestRestore_DryRunFirstBrowserSurfaceShowsNewPane(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)

	layout := &model.Layout{
		Name:    "browser-first",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "browser",
				CWD:   "/tmp/project",
				Index: 0,
				Panes: []model.Pane{
					{
						Type: "browser",
						Surfaces: []model.Surface{
							{Type: "browser", URL: "http://localhost:3000", Selected: true},
						},
					},
				},
			},
		},
	}
	_ = store.Save("browser-first", layout)

	restorer := &Restorer{Client: &mockClient{}, Store: store}
	result, err := restorer.Restore("browser-first", true, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore dry-run: %v", err)
	}

	hasWarning := false
	hasNewBrowserPane := false
	for _, cmd := range result.Commands {
		if containsStr(cmd, "first browser surface restores as an additional pane") {
			hasWarning = true
		}
		if containsStr(cmd, "new-pane --type browser") && containsStr(cmd, "http://localhost:3000") {
			hasNewBrowserPane = true
		}
	}
	if !hasWarning {
		t.Error("missing dry-run browser-first warning")
	}
	if !hasNewBrowserPane {
		t.Error("missing dry-run browser new-pane command")
	}
}

func TestRestore_DryRunReplaysFocusTargetsInPaneOrder(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)

	layout := &model.Layout{
		Name:    "layout-order",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "layout",
				CWD:   "/tmp/project",
				Index: 0,
				Panes: []model.Pane{
					{Index: 0, Type: "terminal", Focus: true},
					{Index: 2, Type: "terminal", Split: "right", FocusTarget: 0},
					{Index: 1, Type: "terminal", Split: "down", FocusTarget: 0},
				},
			},
		},
	}
	if err := store.Save("layout-order", layout); err != nil {
		t.Fatalf("save layout: %v", err)
	}

	restorer := &Restorer{Client: &mockClient{}, Store: store}
	result, err := restorer.Restore("layout-order", true, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore dry-run: %v", err)
	}

	firstFocus := commandIndex(result.Commands, "focus-pane --pane pane:0")
	rightSplit := commandIndex(result.Commands, "new-split right")
	secondFocus := commandIndexAfter(result.Commands, "focus-pane --pane pane:0", rightSplit)
	downSplit := commandIndex(result.Commands, "new-split down")
	if firstFocus < 0 || rightSplit < 0 || secondFocus < 0 || downSplit < 0 {
		t.Fatalf("missing layout replay commands: %#v", result.Commands)
	}
	if !(firstFocus < rightSplit && rightSplit < secondFocus && secondFocus < downSplit) {
		t.Fatalf("unexpected replay order: focus=%d right=%d focus2=%d down=%d commands=%#v",
			firstFocus, rightSplit, secondFocus, downSplit, result.Commands)
	}
}

func TestRestore_DryRunReplaysTerminalWorkingDirectories(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)

	layout := &model.Layout{
		Name:    "cwd-restore",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "cwd",
				CWD:   "/tmp/project",
				Index: 0,
				Panes: []model.Pane{
					{
						Index: 0,
						Type:  "terminal",
						Surfaces: []model.Surface{
							{Type: "terminal", CWD: "/tmp/project", Selected: true},
						},
					},
					{
						Index: 1,
						Type:  "terminal",
						Split: "right",
						Surfaces: []model.Surface{
							{Type: "terminal", CWD: "/tmp/other package", Command: "npm test", Selected: true},
						},
					},
				},
			},
		},
	}
	if err := store.Save("cwd-restore", layout); err != nil {
		t.Fatalf("save layout: %v", err)
	}

	restorer := &Restorer{Client: &mockClient{}, Store: store}
	result, err := restorer.Restore("cwd-restore", true, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore dry-run: %v", err)
	}

	cdIndex := commandIndex(result.Commands, "cd '/tmp/other package'")
	startIndex := commandIndex(result.Commands, "npm test")
	if cdIndex < 0 || startIndex < 0 {
		t.Fatalf("missing cwd restore commands: %#v", result.Commands)
	}
	if cdIndex > startIndex {
		t.Fatalf("cwd command should precede startup command: %#v", result.Commands)
	}
	if sameCWDIndex := commandIndex(result.Commands, "cd '/tmp/project'"); sameCWDIndex >= 0 {
		t.Fatalf("workspace cwd should not emit redundant cd: %#v", result.Commands)
	}
}

func TestRestore_DryRunResumesCodexSession(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)

	layout := &model.Layout{
		Name:    "codex-resume",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "codex",
				CWD:   "/tmp",
				Index: 0,
				Panes: []model.Pane{
					{
						Index: 0,
						Type:  "terminal",
						Surfaces: []model.Surface{
							{
								Type:     "terminal",
								CWD:      "/tmp/codex repo",
								Selected: true,
								Agent: &model.AgentSession{
									Kind:      "codex",
									SessionID: "codex-session-1",
								},
							},
						},
					},
				},
			},
		},
	}
	if err := store.Save("codex-resume", layout); err != nil {
		t.Fatalf("save layout: %v", err)
	}

	restorer := &Restorer{Client: &mockClient{}, Store: store}
	result, err := restorer.Restore("codex-resume", true, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore dry-run: %v", err)
	}

	cdIndex := commandIndex(result.Commands, "cd '/tmp/codex repo'")
	resumeIndex := commandIndex(result.Commands, "codex resume 'codex-session-1'")
	if cdIndex < 0 || resumeIndex < 0 {
		t.Fatalf("missing codex resume commands: %#v", result.Commands)
	}
	if cdIndex > resumeIndex {
		t.Fatalf("cwd command should precede codex resume: %#v", result.Commands)
	}
}

func TestRestore_LayoutNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	mc := &mockClient{}

	restorer := &Restorer{Client: mc, Store: store}
	_, err := restorer.Restore("nonexistent", false, RestoreModeAdd)
	if err == nil {
		t.Error("expected error for nonexistent layout")
	}
}

func TestRestore_RemoteWorkspaceUsesCmuxSSH(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	layout := &model.Layout{
		Name:    "remote",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "gpu-box",
				CWD:   "/home/dev/project",
				Index: 0,
				Remote: &model.RemoteWorkspace{
					Enabled:         true,
					Provider:        "cmux_ssh",
					Destination:     "dev@gpu-box",
					Port:            2222,
					IdentityFile:    "~/.ssh/id_ed25519",
					SSHOptions:      []string{"StrictHostKeyChecking=accept-new"},
					CaptureComplete: true,
				},
				Panes: []model.Pane{
					{Type: "terminal", Focus: true, Surfaces: []model.Surface{{Type: "terminal", Selected: true}}},
				},
			},
		},
	}
	if err := store.Save("remote", layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    &client.TreeResponse{},
			sidebarCWDs: map[string]string{},
		},
	}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("remote", false, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.WorkspacesOK != 1 {
		t.Fatalf("WorkspacesOK = %d, want 1; errors=%v", result.WorkspacesOK, result.Errors)
	}
	if len(mc.remoteCreated) != 1 {
		t.Fatalf("remoteCreated = %d, want 1", len(mc.remoteCreated))
	}
	opts := mc.remoteCreated[0]
	if opts.Destination != "dev@gpu-box" || opts.Port != 2222 || opts.IdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("remote opts = %#v", opts)
	}
	if len(opts.SSHOptions) != 1 || opts.SSHOptions[0] != "StrictHostKeyChecking=accept-new" {
		t.Errorf("SSHOptions = %#v", opts.SSHOptions)
	}
	if !opts.NoFocus {
		t.Error("NoFocus should be true so restore controls selection")
	}
}

func TestRestore_RemoteBrowserWaitsForProxyStatus(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	layout := &model.Layout{
		Name:    "remote-browser",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "gpu-box",
				CWD:   "/home/dev/project",
				Index: 0,
				Remote: &model.RemoteWorkspace{
					Enabled:         true,
					Provider:        "cmux_ssh",
					Destination:     "dev@gpu-box",
					CaptureComplete: true,
				},
				Panes: []model.Pane{
					{Type: "terminal", Focus: true, Surfaces: []model.Surface{{Type: "terminal", Selected: true}}},
					{Type: "browser", Split: "right", URL: "http://localhost:3000", Index: 1},
				},
			},
		},
	}
	if err := store.Save("remote-browser", layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    &client.TreeResponse{},
			sidebarCWDs: map[string]string{},
		},
		remoteStatuses: []client.RemoteStatusPayload{
			{Enabled: true, State: "connected", Proxy: client.RemoteProxyPayload{State: "ready"}},
		},
	}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("remote-browser", false, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.WorkspacesOK != 1 {
		t.Fatalf("WorkspacesOK = %d, errors=%v", result.WorkspacesOK, result.Errors)
	}
	if len(mc.remoteStatusCalls) != 1 || mc.remoteStatusCalls[0] != "remote-new-id" {
		t.Fatalf("RemoteStatus calls = %#v, want remote-new-id", mc.remoteStatusCalls)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
	if len(mc.panesCreated) != 1 || mc.panesCreated[0].Type != "browser" {
		t.Fatalf("panesCreated = %#v, want browser pane", mc.panesCreated)
	}
}

func TestRestore_RemoteBrowserProxyStatusFallsBackToWorkspaceRef(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	layout := &model.Layout{
		Name:    "remote-browser-ref",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "gpu-box",
				CWD:   "/home/dev/project",
				Index: 0,
				Remote: &model.RemoteWorkspace{
					Enabled:         true,
					Provider:        "cmux_ssh",
					Destination:     "dev@gpu-box",
					CaptureComplete: true,
				},
				Panes: []model.Pane{
					{Type: "terminal", Focus: true, Surfaces: []model.Surface{{Type: "terminal", Selected: true}}},
					{Type: "browser", Split: "right", URL: "http://localhost:3000", Index: 1},
				},
			},
		},
	}
	if err := store.Save("remote-browser-ref", layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    &client.TreeResponse{},
			sidebarCWDs: map[string]string{},
		},
		remoteIDSet: true,
		remoteStatuses: []client.RemoteStatusPayload{
			{Enabled: true, State: "connected", Proxy: client.RemoteProxyPayload{State: "ready"}},
		},
	}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("remote-browser-ref", false, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.WorkspacesOK != 1 {
		t.Fatalf("WorkspacesOK = %d, errors=%v", result.WorkspacesOK, result.Errors)
	}
	if len(mc.remoteStatusCalls) != 1 || mc.remoteStatusCalls[0] != "workspace:remote-new" {
		t.Fatalf("RemoteStatus calls = %#v, want workspace ref fallback", mc.remoteStatusCalls)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
}

func TestRestore_BrowserPaneUsesPaneCreate(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	layout := &model.Layout{
		Name:    "browser",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "dev",
				CWD:   "/tmp/project",
				Index: 0,
				Panes: []model.Pane{
					{Type: "terminal", Focus: true},
					{Type: "browser", Split: "right", URL: "http://localhost:3000", Index: 1},
				},
			},
		},
	}
	if err := store.Save("browser", layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp:    &client.TreeResponse{},
			sidebarCWDs: map[string]string{},
		},
	}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("browser", false, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.WorkspacesOK != 1 {
		t.Fatalf("WorkspacesOK = %d, errors=%v", result.WorkspacesOK, result.Errors)
	}
	if len(mc.panesCreated) != 1 {
		t.Fatalf("panesCreated = %d, want 1", len(mc.panesCreated))
	}
	pane := mc.panesCreated[0]
	if pane.Type != "browser" || pane.URL != "http://localhost:3000" || pane.Direction != "right" {
		t.Errorf("pane create opts = %#v", pane)
	}
}

func TestRestore_AdditionalSurfaceUsesSurfaceCreate(t *testing.T) {
	dir := t.TempDir()
	store, _ := persist.NewFileStore(dir)
	layout := &model.Layout{
		Name:    "surfaces",
		Version: 1,
		SavedAt: time.Now().UTC(),
		Workspaces: []model.Workspace{
			{
				Title: "dev",
				CWD:   "/tmp/project",
				Index: 0,
				Panes: []model.Pane{
					{
						Type:  "terminal",
						Focus: true,
						Index: 0,
						Surfaces: []model.Surface{
							{Type: "terminal", Selected: true},
							{Type: "browser", URL: "http://localhost:3000", Index: 1},
						},
					},
				},
			},
		},
	}
	if err := store.Save("surfaces", layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	mc := &mockCmuxClient{
		mockClient: mockClient{
			treeResp: &client.TreeResponse{
				Windows: []client.TreeWindow{
					{Workspaces: []client.TreeWorkspace{
						{Ref: "workspace:new", Panes: []client.TreePane{{Ref: "pane:1", Index: 0}}},
					}},
				},
			},
			sidebarCWDs: map[string]string{},
		},
	}
	restorer := &Restorer{Client: mc, Store: store}

	result, err := restorer.Restore("surfaces", false, RestoreModeAdd)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.WorkspacesOK != 1 {
		t.Fatalf("WorkspacesOK = %d, errors=%v", result.WorkspacesOK, result.Errors)
	}
	if len(mc.surfacesCreated) != 1 {
		t.Fatalf("surfacesCreated = %d, want 1", len(mc.surfacesCreated))
	}
	surface := mc.surfacesCreated[0]
	if surface.Type != "browser" || surface.PaneRef != "pane:1" || surface.URL != "http://localhost:3000" {
		t.Errorf("surface create opts = %#v", surface)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func commandIndex(commands []string, substr string) int {
	return commandIndexAfter(commands, substr, -1)
}

func commandIndexAfter(commands []string, substr string, after int) int {
	for i := after + 1; i < len(commands); i++ {
		if containsStr(commands[i], substr) {
			return i
		}
	}
	return -1
}
