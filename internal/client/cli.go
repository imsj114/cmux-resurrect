package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CLIClient implements Backend by exec'ing the cmux binary.
type CLIClient struct {
	Binary  string
	Timeout time.Duration
}

// NewCLIClient creates a CLIClient with sensible defaults.
func NewCLIClient() *CLIClient {
	return &CLIClient{
		Binary:  "cmux",
		Timeout: 10 * time.Second,
	}
}

func (c *CLIClient) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cmux %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *CLIClient) runJSON(out any, args ...string) error {
	raw, err := c.run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("parse cmux JSON: %w\n%s", err, raw)
	}
	return nil
}

func (c *CLIClient) rpc(method string, params any, out any) error {
	args := []string{"rpc", method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal rpc params: %w", err)
		}
		args = append(args, string(data))
	}
	return c.runJSON(out, args...)
}

func (c *CLIClient) Ping() error {
	_, err := c.run("ping")
	return err
}

func (c *CLIClient) Tree() (*TreeResponse, error) {
	out, err := c.run("--id-format", "both", "tree", "--json")
	if err != nil {
		out, err = c.run("tree", "--json")
		if err != nil {
			return nil, err
		}
	}
	var resp TreeResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parse tree JSON: %w", err)
	}
	return &resp, nil
}

func (c *CLIClient) SidebarState(workspaceRef string) (*SidebarState, error) {
	out, err := c.run("sidebar-state", "--workspace", workspaceRef)
	if err != nil {
		return nil, err
	}
	return ParseSidebarState(out)
}

func (c *CLIClient) ListWorkspaces() ([]WorkspaceInfo, error) {
	out, err := c.run("list-workspaces")
	if err != nil {
		return nil, err
	}
	return ParseListWorkspaces(out)
}

func (c *CLIClient) NewWorkspace(opts NewWorkspaceOpts) (string, error) {
	// Snapshot existing workspace refs before creation.
	before := make(map[string]bool)
	if wsList, err := c.ListWorkspaces(); err == nil {
		for _, w := range wsList {
			before[w.Ref] = true
		}
	}

	args := []string{"new-workspace"}
	if opts.CWD != "" {
		args = append(args, "--cwd", opts.CWD)
	}
	if opts.Command != "" {
		args = append(args, "--command", opts.Command)
	}
	_, err := c.run(args...)
	if err != nil {
		return "", err
	}

	// Poll list-workspaces and find the NEW ref (not in the before set).
	var ref string
	deadline := time.Now().Add(NewWorkspaceDeadline)
	for time.Now().Before(deadline) {
		ws, err := c.ListWorkspaces()
		if err != nil {
			time.Sleep(PollInterval)
			continue
		}
		for _, w := range ws {
			if !before[w.Ref] {
				ref = w.Ref
				break
			}
		}
		if ref != "" {
			break
		}
		time.Sleep(PollInterval)
	}
	if ref == "" {
		return "", fmt.Errorf("new workspace created but could not determine ref")
	}
	return ref, nil
}

func (c *CLIClient) RenameWorkspace(ref, title string) error {
	_, err := c.run("rename-workspace", "--workspace", ref, title)
	return err
}

func (c *CLIClient) SelectWorkspace(ref string) error {
	_, err := c.run("select-workspace", "--workspace", ref)
	return err
}

func (c *CLIClient) PinWorkspace(ref string) error {
	_, err := c.run("workspace-action", "--action", "pin", "--workspace", ref)
	return err
}

func (c *CLIClient) CloseWorkspace(ref string) error {
	_, err := c.run("close-workspace", "--workspace", ref)
	return err
}

func (c *CLIClient) NewSplit(direction, workspaceRef string) (string, error) {
	// Snapshot surface refs before split so we can detect the new one.
	before := make(map[string]bool)
	if workspaceRef != "" {
		if tree, err := c.Tree(); err == nil {
			for _, w := range tree.Windows {
				for _, ws := range w.Workspaces {
					if ws.Ref != workspaceRef {
						continue
					}
					for _, p := range ws.Panes {
						for _, s := range p.Surfaces {
							before[s.Ref] = true
						}
					}
				}
			}
		}
	}

	args := []string{"new-split", direction}
	if workspaceRef != "" {
		args = append(args, "--workspace", workspaceRef)
	}
	if _, err := c.run(args...); err != nil {
		return "", err
	}

	// Find the new surface by diffing against the snapshot.
	if workspaceRef != "" {
		deadline := time.Now().Add(NewSplitDeadline)
		for time.Now().Before(deadline) {
			time.Sleep(PollInterval)
			tree, err := c.Tree()
			if err != nil {
				continue
			}
			for _, w := range tree.Windows {
				for _, ws := range w.Workspaces {
					if ws.Ref != workspaceRef {
						continue
					}
					for _, p := range ws.Panes {
						for _, s := range p.Surfaces {
							if !before[s.Ref] {
								return s.Ref, nil
							}
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("split created but could not determine new surface ref")
}

func (c *CLIClient) FocusPane(paneRef, workspaceRef string) error {
	args := []string{"focus-pane", "--pane", paneRef}
	if workspaceRef != "" {
		args = append(args, "--workspace", workspaceRef)
	}
	_, err := c.run(args...)
	return err
}

func (c *CLIClient) DryRunFormatter() DryRunFormatter { return CmuxDryRun{} }

func (c *CLIClient) Send(workspaceRef, surfaceRef, text string) error {
	args := []string{"send"}
	if workspaceRef != "" {
		args = append(args, "--workspace", workspaceRef)
	}
	if surfaceRef != "" {
		args = append(args, "--surface", surfaceRef)
	}
	args = append(args, text)
	_, err := c.run(args...)
	return err
}

func (c *CLIClient) WorkspaceListJSON() (*WorkspaceListResponse, error) {
	var resp WorkspaceListResponse
	if err := c.rpc("workspace.list", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *CLIClient) PaneListJSON(workspaceRef string) (*PaneListResponse, error) {
	args := []string{"--json", "list-panes"}
	if workspaceRef != "" {
		args = append(args, "--workspace", workspaceRef)
	}

	var resp PaneListResponse
	if err := c.runJSON(&resp, args...); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *CLIClient) TerminalListJSON() (*TerminalListResponse, error) {
	var resp TerminalListResponse
	if err := c.rpc("debug.terminals", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *CLIClient) RemoteStatus(workspaceID string) (*RemoteStatusPayload, error) {
	var resp struct {
		Remote RemoteStatusPayload `json:"remote"`
	}
	if err := c.rpc("workspace.remote.status", map[string]any{"workspace_id": workspaceID}, &resp); err != nil {
		return nil, err
	}
	return &resp.Remote, nil
}

func (c *CLIClient) NewRemoteWorkspace(opts RemoteSSHOpts) (string, string, error) {
	args := []string{"--json", "ssh", opts.Destination}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Port > 0 {
		args = append(args, "--port", fmt.Sprintf("%d", opts.Port))
	}
	if opts.IdentityFile != "" {
		args = append(args, "--identity", opts.IdentityFile)
	}
	for _, opt := range opts.SSHOptions {
		if strings.TrimSpace(opt) == "" {
			continue
		}
		args = append(args, "--ssh-option", opt)
	}
	if opts.NoFocus {
		args = append(args, "--no-focus")
	}

	var resp struct {
		WorkspaceID  string `json:"workspace_id"`
		WorkspaceRef string `json:"workspace_ref"`
	}
	if err := c.runJSON(&resp, args...); err != nil {
		return "", "", err
	}
	ref := resp.WorkspaceRef
	if ref == "" {
		ref = resp.WorkspaceID
	}
	if ref == "" {
		return "", "", fmt.Errorf("cmux ssh created workspace but returned no workspace ref")
	}
	return ref, resp.WorkspaceID, nil
}

func (c *CLIClient) NewPane(opts PaneCreateOpts) (string, string, error) {
	args := []string{"--json", "new-pane"}
	if opts.Type != "" {
		args = append(args, "--type", opts.Type)
	}
	if opts.Direction != "" {
		args = append(args, "--direction", opts.Direction)
	}
	if opts.WorkspaceRef != "" {
		args = append(args, "--workspace", opts.WorkspaceRef)
	}
	if opts.URL != "" {
		args = append(args, "--url", opts.URL)
	}

	var resp struct {
		SurfaceID  string `json:"surface_id"`
		SurfaceRef string `json:"surface_ref"`
		PaneID     string `json:"pane_id"`
		PaneRef    string `json:"pane_ref"`
	}
	if err := c.runJSON(&resp, args...); err != nil {
		return "", "", err
	}
	surfaceRef := resp.SurfaceRef
	if surfaceRef == "" {
		surfaceRef = resp.SurfaceID
	}
	paneRef := resp.PaneRef
	if paneRef == "" {
		paneRef = resp.PaneID
	}
	if surfaceRef == "" {
		return "", "", fmt.Errorf("new pane created but returned no surface ref")
	}
	return surfaceRef, paneRef, nil
}

func (c *CLIClient) NewSurface(opts PaneCreateOpts) (string, error) {
	args := []string{"--json", "new-surface"}
	if opts.Type != "" {
		args = append(args, "--type", opts.Type)
	}
	if opts.PaneRef != "" {
		args = append(args, "--pane", opts.PaneRef)
	}
	if opts.WorkspaceRef != "" {
		args = append(args, "--workspace", opts.WorkspaceRef)
	}
	if opts.URL != "" {
		args = append(args, "--url", opts.URL)
	}

	var resp struct {
		SurfaceID  string `json:"surface_id"`
		SurfaceRef string `json:"surface_ref"`
	}
	if err := c.runJSON(&resp, args...); err != nil {
		return "", err
	}
	surfaceRef := resp.SurfaceRef
	if surfaceRef == "" {
		surfaceRef = resp.SurfaceID
	}
	if surfaceRef == "" {
		return "", fmt.Errorf("new surface created but returned no surface ref")
	}
	return surfaceRef, nil
}
