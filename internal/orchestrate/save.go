package orchestrate

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/drolosoft/cmux-resurrect/internal/client"
	"github.com/drolosoft/cmux-resurrect/internal/model"
	"github.com/drolosoft/cmux-resurrect/internal/persist"
)

// Saver captures the current cmux state and persists it.
type Saver struct {
	Client     client.Backend
	Store      persist.Store
	RemoteOnly bool
}

type workspaceMetadataIndex struct {
	byID    map[string]client.WorkspaceRow
	byRef   map[string]client.WorkspaceRow
	byTitle map[string]client.WorkspaceRow
}

type terminalMetadataIndex struct {
	byID  map[string]client.TerminalRow
	byRef map[string]client.TerminalRow
}

// Save captures the live cmux state and writes it to the store.
func (s *Saver) Save(name, description string) (*model.Layout, error) {
	tree, err := s.Client.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	if len(tree.Windows) == 0 {
		return nil, fmt.Errorf("no windows found")
	}

	// Use the first (typically only) window.
	win := tree.Windows[0]

	layout := &model.Layout{
		Name:        name,
		Description: description,
		Version:     1,
		SavedAt:     time.Now().UTC(),
	}

	metadata := s.workspaceMetadata()
	terminalMetadata := s.terminalMetadata()
	agentSessions := loadAgentSessionIndex()
	existing := s.existingLayout(name)
	existingWorkspaces := indexExistingWorkspaces(existing)
	topologyCaptured := make(map[string]bool)
	for _, tw := range win.Workspaces {
		row, hasRow := metadata.find(tw)
		ws, captured, err := s.buildWorkspace(tw, row, hasRow, terminalMetadata, agentSessions, existingWorkspaces[tw.Title])
		if err != nil {
			// Log but don't fail — isolate errors per workspace.
			fmt.Fprintf(os.Stderr, "  warning: workspace %q: %v\n", tw.Title, err)
			continue
		}
		if s.RemoteOnly && !isRemoteWorkspaceModel(ws) {
			continue
		}
		topologyCaptured[ws.Title] = captured
		layout.Workspaces = append(layout.Workspaces, *ws)
	}

	if len(layout.Workspaces) == 0 {
		if s.RemoteOnly {
			return nil, fmt.Errorf("no remote workspaces could be captured")
		}
		return nil, fmt.Errorf("no workspaces could be captured")
	}

	// If a TOML already exists, merge user-edited fields (split direction, commands).
	if existing != nil {
		mergeUserEdits(layout, existing, topologyCaptured)
	}

	if err := s.Store.Save(name, layout); err != nil {
		return nil, fmt.Errorf("save layout: %w", err)
	}
	return layout, nil
}

func (s *Saver) existingLayout(name string) *model.Layout {
	existing, err := s.Store.Load(name)
	if err != nil {
		return nil
	}
	return existing
}

func indexExistingWorkspaces(layout *model.Layout) map[string]*model.Workspace {
	byTitle := make(map[string]*model.Workspace)
	if layout == nil {
		return byTitle
	}
	for i := range layout.Workspaces {
		if layout.Workspaces[i].Title != "" {
			byTitle[layout.Workspaces[i].Title] = &layout.Workspaces[i]
		}
	}
	return byTitle
}

func (s *Saver) workspaceMetadata() workspaceMetadataIndex {
	idx := workspaceMetadataIndex{
		byID:    make(map[string]client.WorkspaceRow),
		byRef:   make(map[string]client.WorkspaceRow),
		byTitle: make(map[string]client.WorkspaceRow),
	}
	remoteClient, ok := s.Client.(client.CmuxRemoteBackend)
	if !ok {
		return idx
	}
	resp, err := remoteClient.WorkspaceListJSON()
	if err != nil || resp == nil {
		return idx
	}
	for _, row := range resp.Workspaces {
		if row.ID != "" {
			idx.byID[row.ID] = row
		}
		if row.Ref != "" {
			idx.byRef[row.Ref] = row
		}
		if row.Title != "" {
			idx.byTitle[row.Title] = row
		}
	}
	return idx
}

func (s *Saver) terminalMetadata() terminalMetadataIndex {
	idx := terminalMetadataIndex{
		byID:  make(map[string]client.TerminalRow),
		byRef: make(map[string]client.TerminalRow),
	}
	remoteClient, ok := s.Client.(client.CmuxRemoteBackend)
	if !ok {
		return idx
	}
	resp, err := remoteClient.TerminalListJSON()
	if err != nil || resp == nil {
		return idx
	}
	for _, row := range resp.Terminals {
		if row.SurfaceID != "" {
			idx.byID[row.SurfaceID] = row
		}
		if row.SurfaceRef != "" {
			idx.byRef[row.SurfaceRef] = row
		}
	}
	return idx
}

func (idx workspaceMetadataIndex) find(tw client.TreeWorkspace) (client.WorkspaceRow, bool) {
	if tw.ID != "" {
		if row, ok := idx.byID[tw.ID]; ok {
			return row, true
		}
	}
	if tw.Ref != "" {
		if row, ok := idx.byRef[tw.Ref]; ok {
			return row, true
		}
	}
	if tw.Title != "" {
		if row, ok := idx.byTitle[tw.Title]; ok {
			return row, true
		}
	}
	return client.WorkspaceRow{}, false
}

func (s *Saver) buildWorkspace(tw client.TreeWorkspace, row client.WorkspaceRow, hasRow bool, terminals terminalMetadataIndex, agents agentSessionIndex, existing *model.Workspace) (*model.Workspace, bool, error) {
	// Get CWD from sidebar-state.
	sidebar, err := s.Client.SidebarState(tw.Ref)
	if err != nil {
		return nil, false, fmt.Errorf("sidebar-state: %w", err)
	}

	cwd := sidebar.CWD
	if hasRow && strings.TrimSpace(row.CurrentDirectory) != "" {
		cwd = row.CurrentDirectory
	}
	isRemote := hasRow && row.Remote.Enabled
	if isRemote {
		cwd = "~"
	}

	ws := &model.Workspace{
		Title:  tw.Title,
		CWD:    cwd,
		Pinned: tw.Pinned,
		Index:  tw.Index,
		Active: tw.Active || tw.Selected,
	}
	if isRemote {
		ws.Remote = remoteWorkspaceFromStatus(row.Remote)
		if existing != nil {
			mergeRemoteReplayFields(ws.Remote, existing.Remote)
		}
	}

	workspaceAgents := agents
	if isRemoteWorkspaceModel(ws) {
		workspaceAgents = agents.withRemote(ws.Remote)
	}

	// Sort panes by index.
	panes := make([]client.TreePane, len(tw.Panes))
	copy(panes, tw.Panes)
	sort.Slice(panes, func(i, j int) bool {
		return panes[i].Index < panes[j].Index
	})

	for i, tp := range panes {
		pane := model.Pane{
			Type:  "terminal",
			Focus: tp.Focused,
			Index: tp.Index,
		}

		// First pane has no split direction; subsequent default to "right".
		if i > 0 {
			pane.Split = "right"
		}

		pane.Surfaces = buildSurfaces(tp, terminals, workspaceAgents, isRemoteWorkspaceModel(ws))
		mirrorSelectedSurface(&pane, tp)

		ws.Panes = append(ws.Panes, pane)
	}

	// Ensure at least one pane.
	if len(ws.Panes) == 0 {
		ws.Panes = []model.Pane{{Type: "terminal", Focus: true}}
	}

	topologyCaptured := s.applyPaneTopology(ws, tw.Ref, panes)
	return ws, topologyCaptured, nil
}

func remoteWorkspaceFromStatus(status client.RemoteStatusPayload) *model.RemoteWorkspace {
	remote := &model.RemoteWorkspace{
		Enabled:         true,
		Provider:        "cmux_ssh",
		Destination:     status.Destination,
		HasIdentityFile: status.HasIdentityFile,
		HasSSHOptions:   status.HasSSHOptions,
		CaptureComplete: true,
	}
	if status.Port != nil {
		remote.Port = *status.Port
	}
	finalizeRemoteReplay(remote)
	return remote
}

func buildSurfaces(tp client.TreePane, terminals terminalMetadataIndex, agents agentSessionIndex, remote bool) []model.Surface {
	surfaces := make([]client.TreeSurface, len(tp.Surfaces))
	copy(surfaces, tp.Surfaces)
	sort.Slice(surfaces, func(i, j int) bool {
		left := surfaces[i].IndexInPane
		right := surfaces[j].IndexInPane
		if left == right {
			return surfaces[i].Index < surfaces[j].Index
		}
		return left < right
	})

	result := make([]model.Surface, 0, len(surfaces))
	for _, surf := range surfaces {
		item := model.Surface{
			Type:     surfaceType(surf.Type),
			Title:    surf.Title,
			Index:    surf.IndexInPane,
			Selected: surf.Selected || surf.SelectedInPane || surf.Ref == tp.SelectedSurfaceRef,
		}
		if surf.URL != nil {
			item.URL = *surf.URL
		}
		if item.Type != "browser" {
			if remote {
				item.CWD = remoteCWDFromTitle(item.Title)
			} else {
				item.CWD = terminals.cwdFor(surf)
			}
			if agentRecord, ok := agents.recordFor(surf.ID); ok {
				agent := agentRecord.agent
				item.Agent = &agent
				if item.CWD == "" {
					item.CWD = agentRecord.cwd
				}
			}
		}
		result = append(result, item)
	}
	return result
}

func isRemoteWorkspaceModel(ws *model.Workspace) bool {
	return ws != nil && ws.Remote != nil && ws.Remote.Enabled
}

func remoteCWDFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	idx := strings.LastIndex(title, ": ")
	if idx < 0 {
		return ""
	}
	cwd := strings.TrimSpace(title[idx+2:])
	if cwd == "" {
		return ""
	}
	if strings.HasPrefix(cwd, "/") || strings.HasPrefix(cwd, "~") {
		return cwd
	}
	return ""
}

func (idx terminalMetadataIndex) cwdFor(surface client.TreeSurface) string {
	var row client.TerminalRow
	var ok bool
	if surface.ID != "" {
		row, ok = idx.byID[surface.ID]
	}
	if !ok && surface.Ref != "" {
		row, ok = idx.byRef[surface.Ref]
	}
	if !ok {
		return ""
	}
	if cwd := strings.TrimSpace(row.CurrentDirectory); cwd != "" {
		return cwd
	}
	return strings.TrimSpace(row.RequestedWorkingDirectory)
}

func mirrorSelectedSurface(pane *model.Pane, tp client.TreePane) {
	if len(pane.Surfaces) == 0 {
		return
	}
	selected := pane.Surfaces[0]
	for _, surf := range pane.Surfaces {
		if surf.Selected {
			selected = surf
			break
		}
	}
	pane.Type = surfaceType(selected.Type)
	pane.CWD = selected.CWD
	pane.URL = selected.URL
	pane.Command = selected.Command
}

func surfaceType(typ string) string {
	if strings.TrimSpace(typ) == "" {
		return "terminal"
	}
	return typ
}

func finalizeRemoteReplay(remote *model.RemoteWorkspace) {
	if remote == nil {
		return
	}
	complete := true
	var missing []string
	if remote.HasIdentityFile && strings.TrimSpace(remote.IdentityFile) == "" {
		complete = false
		missing = append(missing, "identity_file")
	}
	if remote.HasSSHOptions && len(remote.SSHOptions) == 0 {
		complete = false
		missing = append(missing, "ssh_option")
	}
	remote.CaptureComplete = complete
	if complete {
		remote.Warning = ""
		return
	}
	remote.Warning = "cmux hides " + strings.Join(missing, " and ") + "; add replay fields manually for exact restore"
}

func mergeRemoteReplayFields(live, existing *model.RemoteWorkspace) {
	if live == nil || existing == nil {
		return
	}
	if live.IdentityFile == "" {
		live.IdentityFile = existing.IdentityFile
	}
	if len(live.SSHOptions) == 0 && len(existing.SSHOptions) > 0 {
		live.SSHOptions = append([]string(nil), existing.SSHOptions...)
	}
	if live.Warning == "" && existing.Warning != "" {
		live.Warning = existing.Warning
	}
	finalizeRemoteReplay(live)
}

// mergeUserEdits preserves user-edited fields from an existing TOML.
// Fields like split direction, command, and description are kept from existing
// if the user has edited them (since the live tree doesn't expose these).
func mergeUserEdits(live, existing *model.Layout, topologyCaptured map[string]bool) {
	if live.Description == "" && existing.Description != "" {
		live.Description = existing.Description
	}

	// Build index of existing workspaces by title for matching.
	existByTitle := make(map[string]*model.Workspace)
	for i := range existing.Workspaces {
		existByTitle[existing.Workspaces[i].Title] = &existing.Workspaces[i]
	}

	for i := range live.Workspaces {
		lw := &live.Workspaces[i]
		ew, ok := existByTitle[lw.Title]
		if !ok {
			continue
		}
		// Preserve user-set workspace description (live tree doesn't expose it).
		if lw.Description == "" && ew.Description != "" {
			lw.Description = ew.Description
		}
		if lw.Remote != nil && ew.Remote != nil {
			mergeRemoteReplayFields(lw.Remote, ew.Remote)
		}
		// Merge pane-level user edits.
		for j := range lw.Panes {
			if j >= len(ew.Panes) {
				break
			}
			ep := &ew.Panes[j]
			lp := &lw.Panes[j]
			// Preserve user-set split direction only when live geometry could not
			// capture the structure. Geometry is authoritative when available.
			if !topologyCaptured[lw.Title] && ep.Split != "" && ep.Split != "right" {
				lp.Split = ep.Split
			}
			if lp.CWD == "" && ep.CWD != "" {
				lp.CWD = ep.CWD
			}
			// Preserve user-set command.
			if ep.Command != "" {
				lp.Command = ep.Command
				if !hasSurfaceCommand(ep) {
					setSelectedSurfaceCommand(lp, ep.Command)
				}
			}
			mergeSurfaceCommands(lp, ep)
		}
	}
}

func hasSurfaceCommand(pane *model.Pane) bool {
	for _, surface := range pane.Surfaces {
		if surface.Command != "" {
			return true
		}
	}
	return false
}

func setSelectedSurfaceCommand(pane *model.Pane, command string) {
	if len(pane.Surfaces) == 0 {
		return
	}
	for i := range pane.Surfaces {
		if pane.Surfaces[i].Selected {
			pane.Surfaces[i].Command = command
			return
		}
	}
	pane.Surfaces[0].Command = command
}

func mergeSurfaceCommands(live, existing *model.Pane) {
	for i := range live.Surfaces {
		if i >= len(existing.Surfaces) {
			break
		}
		if existing.Surfaces[i].Command != "" {
			live.Surfaces[i].Command = existing.Surfaces[i].Command
			if live.Surfaces[i].Selected {
				live.Command = existing.Surfaces[i].Command
			}
		}
		if live.Surfaces[i].CWD == "" && existing.Surfaces[i].CWD != "" {
			live.Surfaces[i].CWD = existing.Surfaces[i].CWD
			if live.Surfaces[i].Selected && live.CWD == "" {
				live.CWD = existing.Surfaces[i].CWD
			}
		}
		if live.Surfaces[i].Agent == nil && existing.Surfaces[i].Agent != nil {
			agent := *existing.Surfaces[i].Agent
			live.Surfaces[i].Agent = &agent
		}
	}
}
