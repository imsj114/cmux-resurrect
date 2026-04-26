# cmux ssh Restore Support - Implementation Plan

> For agentic workers: implement task-by-task. Keep each task independently testable, preserve existing TOML layouts, and do not weaken the Ghostty backend path while adding cmux-specific remote support.

**Goal:** Save and restore cmux workspaces that were created with `cmux ssh ...`, including their remote connection metadata and browser/terminal panels, so `crex restore` can recreate a usable remote workspace instead of a local placeholder.

**Key constraint:** cmux intentionally hides raw SSH identity paths and SSH options from `workspace.list` and `workspace.remote.status`. crex can capture safe remote fields automatically (`destination`, `port`, booleans that indicate hidden fields exist, proxy/status metadata), but full replay of identities and custom `--ssh-option` values requires user-authored TOML fields or an upstream cmux replay descriptor API.

**Architecture:** Keep `client.Backend` generic. Add cmux-only optional interfaces implemented by `CLIClient` for remote metadata, pane/surface creation, and `cmux ssh --json` launch. The orchestrators type-assert those optional interfaces and fall back to current behavior for Ghostty and older cmux versions.

**Tech stack:** Go, existing Cobra commands, cmux CLI/RPC (`cmux rpc`, `cmux ssh --json`, `cmux new-pane`, `cmux new-surface`), existing TOML store.

---

## Current Behavior

`internal/orchestrate/save.go`
- Calls `Client.Tree()` and `Client.SidebarState()`.
- Builds `model.Workspace` from title, CWD, pin, active state, and pane count.
- Captures only the first surface in each pane.
- Does not read `workspace.list` or `workspace.remote.status`, so remote SSH metadata is lost.

`internal/orchestrate/restore.go`
- Always calls `NewWorkspace({CWD})`.
- Creates additional panes with `NewSplit`.
- Sends saved commands, renames, pins, and focuses.
- Ignores `Pane.Type` and `Pane.URL`, so browser panels are saved but not restored.
- Has no route for `cmux ssh`, so remote workspaces come back as local terminal workspaces.

Relevant cmux APIs verified from current cmux CLI/source:
- `cmux ssh <destination> [--name] [--port] [--identity] [--ssh-option] [--no-focus]`
- `cmux --json ssh ...` returns `workspace_id`, `workspace_ref`, `remote`, `ssh_command`, and bootstrap command fields.
- `cmux rpc workspace.list {}` returns workspace rows with `id`, `ref`, `title`, `current_directory`, and safe `remote` payload.
- `cmux rpc workspace.remote.status {"workspace_id":"..."}` returns safe remote status.
- `cmux new-pane --type terminal|browser --direction ... --workspace ... --url ...`
- `cmux new-surface --type terminal|browser --pane ... --workspace ... --url ...`

---

## Data Model

### Task 1: Add remote metadata to layouts

Files:
- Modify: `internal/model/layout.go`
- Modify tests under `internal/model/`

Add a nested remote descriptor:

```go
type RemoteWorkspace struct {
	Enabled         bool     `toml:"enabled,omitempty"`
	Provider        string   `toml:"provider,omitempty"` // "cmux_ssh"
	Destination     string   `toml:"destination,omitempty"`
	Port            int      `toml:"port,omitempty"`
	IdentityFile    string   `toml:"identity_file,omitempty"`
	SSHOptions      []string `toml:"ssh_option,omitempty"`
	HasIdentityFile bool     `toml:"has_identity_file,omitempty"`
	HasSSHOptions   bool     `toml:"has_ssh_options,omitempty"`
	CaptureComplete bool     `toml:"capture_complete,omitempty"`
	Warning         string   `toml:"warning,omitempty"`
}
```

Add to `Workspace`:

```go
Remote *RemoteWorkspace `toml:"remote,omitempty"`
```

Rules:
- `destination` and `port` are auto-captured.
- `identity_file` and `ssh_option` are preserved if already present in an existing layout, because cmux does not expose them.
- If cmux reports `has_identity_file` or `has_ssh_options` but the layout has no replay fields, set `capture_complete=false` and write a warning.
- Do not persist relay tokens, relay IDs, bootstrap command paths, or proxy port state. Those are per-session runtime artifacts.

### Task 2: Add panel/surface model while keeping backwards compatibility

Files:
- Modify: `internal/model/layout.go`
- Modify: `internal/model/layout_test.go`
- Modify: `internal/persist/store_test.go` as needed

Current `Pane` has a single `Type`, `URL`, and `Command`. Keep those fields for compatibility, but add:

```go
type Surface struct {
	Type     string `toml:"type"`
	Title    string `toml:"title,omitempty"`
	URL      string `toml:"url,omitempty"`
	Command  string `toml:"command,omitempty"`
	Index    int    `toml:"index,omitempty"`
	Selected bool   `toml:"selected,omitempty"`
}

type Pane struct {
	// existing fields stay
	Surfaces []Surface `toml:"surface,omitempty"`
}
```

Compatibility rules:
- New saves populate `Pane.Surfaces`.
- Also mirror the selected/first surface into existing `Pane.Type`, `Pane.URL`, and `Pane.Command` fields so old display code still works.
- Restore treats `Pane.Surfaces == nil` as the old single-surface layout.
- TOML round-trip tests must cover old and new formats.

---

## Client Layer

### Task 3: Add cmux optional interfaces

Files:
- Modify: `internal/client/types.go`
- Modify: `internal/client/cli.go`
- Modify: `internal/client/dryrun.go`

Add cmux-specific types and optional interfaces:

```go
type WorkspaceListResponse struct {
	Workspaces []WorkspaceRow `json:"workspaces"`
}

type WorkspaceRow struct {
	ID               string            `json:"id"`
	Ref              string            `json:"ref"`
	Title            string            `json:"title"`
	Index            int               `json:"index"`
	Selected         bool              `json:"selected"`
	Pinned           bool              `json:"pinned"`
	CurrentDirectory string            `json:"current_directory"`
	Remote           RemoteStatusPayload `json:"remote"`
}

type RemoteStatusPayload struct {
	Enabled         bool   `json:"enabled"`
	State           string `json:"state"`
	Destination     string `json:"destination"`
	Port            *int   `json:"port"`
	HasIdentityFile bool   `json:"has_identity_file"`
	HasSSHOptions   bool   `json:"has_ssh_options"`
}

type RemoteSSHOpts struct {
	Destination  string
	Name         string
	Port         int
	IdentityFile string
	SSHOptions   []string
	NoFocus      bool
}

type PaneCreateOpts struct {
	WorkspaceRef string
	PaneRef      string
	Direction    string
	Type         string
	URL          string
}

type CmuxRemoteBackend interface {
	WorkspaceListJSON() (*WorkspaceListResponse, error)
	RemoteStatus(workspaceID string) (*RemoteStatusPayload, error)
	NewRemoteWorkspace(opts RemoteSSHOpts) (workspaceRef string, workspaceID string, err error)
	NewPane(opts PaneCreateOpts) (surfaceRef string, paneRef string, err error)
	NewSurface(opts PaneCreateOpts) (surfaceRef string, err error)
}
```

Implementation notes:
- Add `CLIClient.rpc(method string, params any, out any) error` using `cmux rpc`.
- `WorkspaceListJSON()` calls `workspace.list`.
- `RemoteStatus()` calls `workspace.remote.status`.
- `NewRemoteWorkspace()` shells out to `cmux --json ssh ... --no-focus` and parses `workspace_id`/`workspace_ref`.
- `NewPane()` uses `cmux new-pane`; `NewSurface()` uses `cmux new-surface`.
- Keep `Backend` unchanged so Ghostty does not need remote-only stubs.

### Task 4: Extend tree IDs

Files:
- Modify: `internal/client/types.go`
- Modify tests/fixtures only if needed

Add optional IDs to the existing tree structs:

```go
ID string `json:"id"`
```

for windows, workspaces, panes, and surfaces. Existing fixtures without IDs should continue to pass.

---

## Save Flow

### Task 5: Capture remote workspace metadata

Files:
- Modify: `internal/orchestrate/save.go`
- Modify: `internal/orchestrate/save_test.go`

Implementation:
1. Before iterating workspaces, type-assert `Client.(client.CmuxRemoteBackend)`.
2. If available, call `WorkspaceListJSON()` once and index rows by `ID`, then by `Ref`, then by `Title`.
3. While building each workspace:
   - Prefer `WorkspaceRow.CurrentDirectory` over sidebar CWD if present.
   - If row remote is enabled, set `Workspace.Remote`.
   - If row remote has `has_identity_file` or `has_ssh_options`, merge existing TOML `identity_file`/`ssh_option` fields by title.
   - Set `capture_complete=false` with a warning when replay fields are missing.
4. Keep the existing per-workspace error isolation.

Merge behavior:
- Extend `mergeUserEdits` to preserve:
  - `Workspace.Remote.IdentityFile`
  - `Workspace.Remote.SSHOptions`
  - `Workspace.Remote.Warning` if live capture cannot improve it
  - Pane/surface commands and split directions as today

### Task 6: Capture all surfaces in each pane

Files:
- Modify: `internal/orchestrate/save.go`
- Modify: `internal/orchestrate/save_test.go`

Implementation:
1. Sort panes by index as today.
2. Sort each pane's surfaces by `IndexInPane`, falling back to `Index`.
3. Create `model.Surface` entries for every surface:
   - `type`
   - `title`
   - `url`
   - `index`
   - `selected`
4. Mirror selected or first surface back to legacy pane fields.
5. Existing tests should still pass; add a fixture/test with two surfaces in one pane and a browser URL.

---

## Restore Flow

### Task 7: Restore remote workspaces through `cmux ssh`

Files:
- Modify: `internal/orchestrate/restore.go`
- Modify: `internal/orchestrate/restore_test.go`

Implementation:
1. In `restoreWorkspace`, branch when `ws.Remote != nil && ws.Remote.Enabled && ws.Remote.Provider == "cmux_ssh"`.
2. Require `client.CmuxRemoteBackend`; otherwise return a clear per-workspace error.
3. Require `destination`; if absent, return a clear per-workspace error.
4. If `capture_complete=false` and hidden fields are missing, restore only safe fields and add a result warning. Do not fail the entire restore.
5. Call `NewRemoteWorkspace(RemoteSSHOpts{Destination, Name: ws.Title, Port, IdentityFile, SSHOptions, NoFocus: true})`.
6. After creation, select the workspace, then restore panes and surfaces.
7. Rename again after shell startup delay to preserve current title behavior.
8. Pin if needed.

Important:
- Do not attempt to reuse saved relay ports, relay IDs, startup script paths, or proxy endpoint values.
- Treat those as volatile runtime details that cmux must regenerate on each `cmux ssh` invocation.

### Task 8: Restore browser panels and multi-surface panes

Files:
- Modify: `internal/orchestrate/restore.go`
- Modify: `internal/orchestrate/restore_test.go`
- Modify: `internal/orchestrate/import.go` and `template_use.go` only if shared helpers are extracted

Implementation:
1. Extract pane creation into helper functions:
   - `restoreFirstPaneSurface`
   - `restoreAdditionalPane`
   - `restoreAdditionalSurface`
2. For old layouts with no `Pane.Surfaces`, synthesize one surface from pane fields.
3. For the first pane:
   - A local workspace already has one terminal.
   - A remote workspace created by `cmux ssh` already has one remote terminal.
   - If the saved first surface is a terminal, send its command to the existing surface.
   - If the saved first surface is a browser, create a browser pane/surface and leave the bootstrap terminal alone, with a warning. cmux ssh workspaces inherently start with a terminal.
4. For additional panes:
   - Terminal: use existing `NewSplit`, so remote workspaces inherit remote terminal behavior.
   - Browser: use `NewPane(type=browser, direction, url)`.
5. For additional surfaces inside an existing pane:
   - Use `NewSurface(type, paneRef, url)`.
6. Restore selected/focused pane at the end.

Remote browser behavior:
- Create browser panels after the workspace has remote configuration.
- Poll `workspace.remote.status` for `state in connected|connecting` and `proxy.state in ready|connecting` with a bounded timeout.
- Do not fail restore solely because the proxy is still bootstrapping; browser panels should be created and cmux can attach proxy settings when ready.

### Task 9: Dry-run output

Files:
- Modify: `internal/client/dryrun.go`
- Modify: `internal/orchestrate/restore.go`
- Modify tests

Add formatter methods or cmux-specific helper strings for:
- `cmux ssh <destination> --name ... --port ... --identity ... --ssh-option ... --no-focus`
- `cmux new-pane --type browser --direction ... --workspace ... --url ...`
- `cmux new-surface --type browser --pane ... --workspace ... --url ...`

Dry-run should print warnings when a remote workspace is only partially replayable because identity/options are hidden.

---

## User-Facing Behavior

### Task 10: Show clear save/restore warnings

Files:
- Modify: `cmd/save.go`
- Modify: `cmd/restore.go`
- Modify: `cmd/show.go`

Expected messages:
- On save: `remote SSH metadata captured for N workspaces; M need identity/ssh_option fields to be fully replayable`
- On show: display `remote: cmux_ssh destination[:port]` and whether replay is complete.
- On restore: show per-workspace warning when restoring without hidden options.

No interactive prompt should block non-interactive restores.

### Task 11: Document TOML remote fields

Files:
- Modify: `docs/commands.md`
- Modify: `docs/configuration.md` or add `docs/remote-ssh.md`
- Modify: `README.md` only if the feature ships

Document:

```toml
[[workspace]]
title = "gpu-box"
cwd = "/home/dev/project"

[workspace.remote]
enabled = true
provider = "cmux_ssh"
destination = "dev@gpu-box"
port = 2222
identity_file = "~/.ssh/id_ed25519"
ssh_option = [
  "StrictHostKeyChecking=accept-new",
  "ControlPath=/tmp/cmux-ssh-%C",
]
```

Explain that `identity_file` and `ssh_option` may need manual entry because cmux status APIs intentionally do not expose raw secret-bearing launch configuration.

---

## Test Plan

### Unit tests

- `internal/model`: TOML round-trip for `remote` and `surface` sections.
- `internal/orchestrate/save`: remote metadata captured from mocked `WorkspaceListJSON`.
- `internal/orchestrate/save`: existing remote replay fields survive re-save.
- `internal/orchestrate/save`: multiple surfaces per pane captured in order.
- `internal/orchestrate/restore`: remote workspace calls `NewRemoteWorkspace`, not `NewWorkspace`.
- `internal/orchestrate/restore`: browser surface calls `NewPane` or `NewSurface`.
- `internal/orchestrate/restore`: partial remote capture produces warning, not global failure.
- `internal/orchestrate/restore`: old layouts without remote/surface sections still restore exactly as before.

### Integration tests

Use cmux test environment variables already used upstream:
- `CMUX_SSH_TEST_HOST`
- `CMUX_SSH_TEST_PORT`
- `CMUX_SSH_TEST_IDENTITY`
- `CMUX_SSH_TEST_OPTIONS`

Scenarios:
1. Create a remote workspace with `cmux ssh`, save, close, restore, assert `workspace.remote.status.enabled == true`.
2. Add a terminal split inside the remote workspace, save/restore, assert new split has remote relay `CMUX_SOCKET_PATH`.
3. Add a browser panel in the remote workspace, save/restore, assert browser URL is restored and remote proxy state is present.
4. Save a workspace with hidden identity/options and no TOML replay fields, assert warning.
5. Add `identity_file` and `ssh_option` manually to TOML, restore, assert `cmux ssh --json` receives those options in the mock client.

Manual smoke:

```sh
cmux ssh dev@host --name gpu-box
cmux new-split right
cmux new-pane --type browser --direction down --url http://localhost:3000
crex save remote-day
crex show remote-day
crex restore remote-day --mode add
cmux rpc workspace.remote.status '{"workspace_id":"<restored-id>"}'
```

---

## Acceptance Criteria

- Existing `go test ./... -count=1` passes.
- Existing non-remote cmux restore behavior is unchanged.
- Existing Ghostty behavior compiles and tests without remote-only requirements.
- A saved `cmux ssh` workspace restores through `cmux ssh`, not through local `new-workspace`.
- Remote destination and port are automatically saved.
- User-provided `identity_file` and `ssh_option` fields are preserved across re-saves and used on restore.
- Browser panels with URLs are restored.
- Multi-surface panes are represented in TOML and restored via `new-surface`.
- Partial remote captures are explicit and actionable, not silent.

---

## Open Questions

1. Should crex add an upstream cmux feature request for an opt-in replay descriptor that exposes the exact non-secret `cmux ssh` flags used to create a workspace?
2. Should `crex save` have an `--allow-sensitive-ssh-metadata` flag if cmux later exposes identity/options?
3. Should remote CWD restoration be best-effort via `cd <cwd>` sent after connect, or should it wait for a cmux remote API that can set initial remote directory?
4. Should browser proxy readiness be a hard restore condition or a warning-only condition?
