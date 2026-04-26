package client

// TreeResponse is the top-level response from `cmux tree --json`.
type TreeResponse struct {
	Caller  *CallerInfo  `json:"caller"`
	Active  *CallerInfo  `json:"active"`
	Windows []TreeWindow `json:"windows"`
}

// CallerInfo identifies the calling terminal context.
type CallerInfo struct {
	WorkspaceRef string `json:"workspace_ref"`
	PaneRef      string `json:"pane_ref"`
	WindowRef    string `json:"window_ref"`
	SurfaceRef   string `json:"surface_ref"`
	SurfaceType  string `json:"surface_type"`
}

// TreeWindow represents a cmux window.
type TreeWindow struct {
	ID                   string          `json:"id"`
	Ref                  string          `json:"ref"`
	Index                int             `json:"index"`
	Active               bool            `json:"active"`
	Visible              bool            `json:"visible"`
	Current              bool            `json:"current"`
	WorkspaceCount       int             `json:"workspace_count"`
	SelectedWorkspaceRef string          `json:"selected_workspace_ref"`
	Workspaces           []TreeWorkspace `json:"workspaces"`
}

// TreeWorkspace represents a cmux workspace in the tree.
type TreeWorkspace struct {
	ID       string     `json:"id"`
	Ref      string     `json:"ref"`
	Title    string     `json:"title"`
	Index    int        `json:"index"`
	Pinned   bool       `json:"pinned"`
	Active   bool       `json:"active"`
	Selected bool       `json:"selected"`
	Panes    []TreePane `json:"panes"`
}

// TreePane represents a pane in the tree.
type TreePane struct {
	ID                 string        `json:"id"`
	Ref                string        `json:"ref"`
	Index              int           `json:"index"`
	Active             bool          `json:"active"`
	Focused            bool          `json:"focused"`
	SurfaceCount       int           `json:"surface_count"`
	SelectedSurfaceRef string        `json:"selected_surface_ref"`
	SurfaceRefs        []string      `json:"surface_refs"`
	Surfaces           []TreeSurface `json:"surfaces"`
}

// TreeSurface represents a surface (terminal or browser) in a pane.
type TreeSurface struct {
	ID             string  `json:"id"`
	Ref            string  `json:"ref"`
	PaneRef        string  `json:"pane_ref"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	URL            *string `json:"url"`
	Index          int     `json:"index"`
	IndexInPane    int     `json:"index_in_pane"`
	Active         bool    `json:"active"`
	Focused        bool    `json:"focused"`
	Selected       bool    `json:"selected"`
	SelectedInPane bool    `json:"selected_in_pane"`
	Here           bool    `json:"here"`
}

// SidebarState holds parsed sidebar-state for a workspace.
type SidebarState struct {
	CWD        string
	FocusedCWD string
	GitBranch  string
	GitDirty   bool
}

// WorkspaceInfo from list-workspaces output.
type WorkspaceInfo struct {
	Ref      string
	Title    string
	Selected bool
}

// NewWorkspaceOpts for creating a new workspace.
type NewWorkspaceOpts struct {
	CWD     string
	Command string
}

// WorkspaceListResponse is the structured response from cmux workspace.list.
type WorkspaceListResponse struct {
	WindowID   string         `json:"window_id"`
	WindowRef  string         `json:"window_ref"`
	Workspaces []WorkspaceRow `json:"workspaces"`
}

// WorkspaceRow contains summary metadata for one cmux workspace.
type WorkspaceRow struct {
	ID               string              `json:"id"`
	Ref              string              `json:"ref"`
	Title            string              `json:"title"`
	Description      string              `json:"description"`
	Index            int                 `json:"index"`
	Selected         bool                `json:"selected"`
	Pinned           bool                `json:"pinned"`
	CurrentDirectory string              `json:"current_directory"`
	Remote           RemoteStatusPayload `json:"remote"`
}

// RemoteStatusPayload is cmux's safe remote workspace status payload.
type RemoteStatusPayload struct {
	Enabled         bool               `json:"enabled"`
	State           string             `json:"state"`
	Destination     string             `json:"destination"`
	Port            *int               `json:"port"`
	HasIdentityFile bool               `json:"has_identity_file"`
	HasSSHOptions   bool               `json:"has_ssh_options"`
	Proxy           RemoteProxyPayload `json:"proxy"`
}

// RemoteProxyPayload is the safe proxy subset in a remote status payload.
type RemoteProxyPayload struct {
	State string `json:"state"`
}

// RemoteSSHOpts describes a cmux ssh workspace to create.
type RemoteSSHOpts struct {
	Destination  string
	Name         string
	Port         int
	IdentityFile string
	SSHOptions   []string
	NoFocus      bool
}

// PaneCreateOpts describes a pane or surface to create in cmux.
type PaneCreateOpts struct {
	WorkspaceRef string
	PaneRef      string
	Direction    string
	Type         string
	URL          string
}

// PaneListResponse is the structured response from `cmux --json list-panes`.
type PaneListResponse struct {
	WorkspaceRef   string    `json:"workspace_ref"`
	WindowRef      string    `json:"window_ref"`
	ContainerFrame RectFrame `json:"container_frame"`
	Panes          []PaneRow `json:"panes"`
}

// RectFrame describes a pane rectangle in screen pixels.
type RectFrame struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// PaneRow contains geometry and identity details for one cmux pane.
type PaneRow struct {
	ID                 string    `json:"id"`
	Ref                string    `json:"ref"`
	Index              int       `json:"index"`
	SurfaceRefs        []string  `json:"surface_refs"`
	SelectedSurfaceRef string    `json:"selected_surface_ref"`
	PixelFrame         RectFrame `json:"pixel_frame"`
}

// CmuxRemoteBackend exposes cmux-only APIs without extending generic backends.
type CmuxRemoteBackend interface {
	WorkspaceListJSON() (*WorkspaceListResponse, error)
	PaneListJSON(workspaceRef string) (*PaneListResponse, error)
	RemoteStatus(workspaceID string) (*RemoteStatusPayload, error)
	NewRemoteWorkspace(opts RemoteSSHOpts) (workspaceRef string, workspaceID string, err error)
	NewPane(opts PaneCreateOpts) (surfaceRef string, paneRef string, err error)
	NewSurface(opts PaneCreateOpts) (surfaceRef string, err error)
}
