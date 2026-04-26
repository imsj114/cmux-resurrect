package client

import (
	"fmt"
	"strings"
)

// DryRunFormatter generates human-readable command strings for dry-run mode.
type DryRunFormatter interface {
	FmtNewWorkspace(cwd string) string
	FmtRenameWorkspace(ref, title string) string
	FmtSelectWorkspace(ref string) string
	FmtNewSplit(direction, ref string) string
	FmtNewRemoteWorkspace(opts RemoteSSHOpts) string
	FmtNewPane(opts PaneCreateOpts) string
	FmtNewSurface(opts PaneCreateOpts) string
	FmtFocusPane(paneRef, workspaceRef string) string
	FmtSend(workspaceRef, text string) string
	FmtPinWorkspace(ref string) string
}

// CmuxDryRun formats dry-run commands as cmux CLI commands.
type CmuxDryRun struct{}

func (CmuxDryRun) FmtNewWorkspace(cwd string) string {
	return fmt.Sprintf("cmux new-workspace --cwd %q", cwd)
}
func (CmuxDryRun) FmtRenameWorkspace(ref, title string) string {
	return fmt.Sprintf("cmux rename-workspace --workspace %s %q", ref, title)
}
func (CmuxDryRun) FmtSelectWorkspace(ref string) string {
	return fmt.Sprintf("cmux select-workspace --workspace %s", ref)
}
func (CmuxDryRun) FmtNewSplit(direction, ref string) string {
	return fmt.Sprintf("cmux new-split %s --workspace %s", direction, ref)
}
func (CmuxDryRun) FmtNewRemoteWorkspace(opts RemoteSSHOpts) string {
	args := []string{"cmux ssh", fmt.Sprintf("%q", opts.Destination)}
	if opts.Name != "" {
		args = append(args, "--name", fmt.Sprintf("%q", opts.Name))
	}
	if opts.Port > 0 {
		args = append(args, "--port", fmt.Sprintf("%d", opts.Port))
	}
	if opts.IdentityFile != "" {
		args = append(args, "--identity", fmt.Sprintf("%q", opts.IdentityFile))
	}
	for _, opt := range opts.SSHOptions {
		if strings.TrimSpace(opt) != "" {
			args = append(args, "--ssh-option", fmt.Sprintf("%q", opt))
		}
	}
	if opts.NoFocus {
		args = append(args, "--no-focus")
	}
	return strings.Join(args, " ")
}
func (CmuxDryRun) FmtNewPane(opts PaneCreateOpts) string {
	typ := opts.Type
	if typ == "" {
		typ = "terminal"
	}
	direction := opts.Direction
	if direction == "" {
		direction = "right"
	}
	cmd := fmt.Sprintf("cmux new-pane --type %s --direction %s --workspace %s", typ, direction, opts.WorkspaceRef)
	if opts.URL != "" {
		cmd += fmt.Sprintf(" --url %q", opts.URL)
	}
	return cmd
}
func (CmuxDryRun) FmtNewSurface(opts PaneCreateOpts) string {
	typ := opts.Type
	if typ == "" {
		typ = "terminal"
	}
	cmd := fmt.Sprintf("cmux new-surface --type %s --pane %s --workspace %s", typ, opts.PaneRef, opts.WorkspaceRef)
	if opts.URL != "" {
		cmd += fmt.Sprintf(" --url %q", opts.URL)
	}
	return cmd
}
func (CmuxDryRun) FmtFocusPane(paneRef, workspaceRef string) string {
	return fmt.Sprintf("cmux focus-pane --pane %s --workspace %s", paneRef, workspaceRef)
}
func (CmuxDryRun) FmtSend(workspaceRef, text string) string {
	return fmt.Sprintf("cmux send --workspace %s %q", workspaceRef, text)
}
func (CmuxDryRun) FmtPinWorkspace(ref string) string {
	return fmt.Sprintf("cmux workspace-action --action pin --workspace %s", ref)
}

// GhosttyDryRun formats dry-run commands as Ghostty AppleScript snippets.
type GhosttyDryRun struct{}

func (GhosttyDryRun) FmtNewWorkspace(cwd string) string {
	return fmt.Sprintf(`osascript: new tab in front window (cwd: %s)`, cwd)
}
func (GhosttyDryRun) FmtRenameWorkspace(ref, title string) string {
	return fmt.Sprintf(`osascript: set_tab_title:%q on %s`, title, ref)
}
func (GhosttyDryRun) FmtSelectWorkspace(ref string) string {
	return fmt.Sprintf(`osascript: select %s`, ref)
}
func (GhosttyDryRun) FmtNewSplit(direction, ref string) string {
	return fmt.Sprintf(`osascript: split %s in %s`, direction, ref)
}
func (GhosttyDryRun) FmtNewRemoteWorkspace(opts RemoteSSHOpts) string {
	return fmt.Sprintf("# cmux ssh restore is not supported by Ghostty backend for %q", opts.Destination)
}
func (GhosttyDryRun) FmtNewPane(opts PaneCreateOpts) string {
	return fmt.Sprintf("# new %s pane in %s is not supported by Ghostty dry-run", opts.Type, opts.WorkspaceRef)
}
func (GhosttyDryRun) FmtNewSurface(opts PaneCreateOpts) string {
	return fmt.Sprintf("# new %s surface in %s is not supported by Ghostty dry-run", opts.Type, opts.PaneRef)
}
func (GhosttyDryRun) FmtFocusPane(paneRef, workspaceRef string) string {
	return fmt.Sprintf(`osascript: focus %s in %s`, paneRef, workspaceRef)
}
func (GhosttyDryRun) FmtSend(workspaceRef, text string) string {
	return fmt.Sprintf(`osascript: input text %q + enter in %s`, text, workspaceRef)
}
func (GhosttyDryRun) FmtPinWorkspace(ref string) string {
	return "# pin: not supported by Ghostty"
}
