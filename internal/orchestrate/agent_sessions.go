package orchestrate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/drolosoft/cmux-resurrect/internal/model"
)

var remoteCodexHookSessionReader = readRemoteCodexHookSessionStore

type agentSessionIndex struct {
	bySurfaceID map[string]agentSessionRecord
}

type agentSessionRecord struct {
	agent     model.AgentSession
	cwd       string
	updatedAt float64
}

type cmuxHookSessionStore struct {
	Sessions map[string]cmuxHookSessionRecord `json:"sessions"`
}

type cmuxHookSessionRecord struct {
	SessionID string  `json:"sessionId"`
	SurfaceID string  `json:"surfaceId"`
	CWD       string  `json:"cwd"`
	UpdatedAt float64 `json:"updatedAt"`
}

func loadAgentSessionIndex() agentSessionIndex {
	idx := newAgentSessionIndex()
	loadLocalCodexHookSessions(&idx)
	return idx
}

func newAgentSessionIndex() agentSessionIndex {
	return agentSessionIndex{bySurfaceID: make(map[string]agentSessionRecord)}
}

func (idx agentSessionIndex) withRemote(remote *model.RemoteWorkspace) agentSessionIndex {
	if remote == nil || !remote.Enabled {
		return idx
	}
	merged := idx.clone()
	loadRemoteCodexHookSessions(&merged, remote)
	return merged
}

func (idx agentSessionIndex) clone() agentSessionIndex {
	cloned := newAgentSessionIndex()
	for surfaceID, record := range idx.bySurfaceID {
		cloned.bySurfaceID[surfaceID] = record
	}
	return cloned
}

func loadLocalCodexHookSessions(idx *agentSessionIndex) {
	path := cmuxAgentHookStatePath("codex")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	loadCodexHookSessionData(idx, data)
}

func loadRemoteCodexHookSessions(idx *agentSessionIndex, remote *model.RemoteWorkspace) {
	if remote == nil || strings.TrimSpace(remote.Destination) == "" {
		return
	}
	data, err := remoteCodexHookSessionReader(remote)
	if err != nil {
		return
	}
	loadCodexHookSessionData(idx, data)
}

func loadCodexHookSessionData(idx *agentSessionIndex, data []byte) {
	var store cmuxHookSessionStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}
	for _, record := range store.Sessions {
		sessionID := strings.TrimSpace(record.SessionID)
		surfaceID := strings.TrimSpace(record.SurfaceID)
		if sessionID == "" || surfaceID == "" {
			continue
		}
		next := agentSessionRecord{
			agent:     model.AgentSession{Kind: "codex", SessionID: sessionID},
			cwd:       strings.TrimSpace(record.CWD),
			updatedAt: record.UpdatedAt,
		}
		if existing, ok := idx.bySurfaceID[surfaceID]; ok && existing.updatedAt > record.UpdatedAt {
			continue
		}
		idx.bySurfaceID[surfaceID] = next
	}
}

func readRemoteCodexHookSessionStore(remote *model.RemoteWorkspace) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := remoteCodexHookSessionSSHArgs(remote)
	return exec.CommandContext(ctx, "ssh", args...).Output()
}

func remoteCodexHookSessionSSHArgs(remote *model.RemoteWorkspace) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=3"}
	if remote.Port > 0 {
		args = append(args, "-p", strconv.Itoa(remote.Port))
	}
	if identityFile := strings.TrimSpace(remote.IdentityFile); identityFile != "" {
		args = append(args, "-i", expandHome(identityFile))
	}
	for _, opt := range remote.SSHOptions {
		if opt = strings.TrimSpace(opt); opt != "" {
			args = append(args, "-o", opt)
		}
	}
	script := `p="${CMUX_AGENT_HOOK_STATE_DIR:-$HOME/.cmuxterm}/codex-hook-sessions.json"; if [ -r "$p" ]; then cat "$p"; fi`
	args = append(args,
		"--",
		strings.TrimSpace(remote.Destination),
		"sh -lc "+shellQuote(script),
	)
	return args
}

func cmuxAgentHookStatePath(agent string) string {
	filename := agent + "-hook-sessions.json"
	if dir := strings.TrimSpace(os.Getenv("CMUX_AGENT_HOOK_STATE_DIR")); dir != "" {
		return filepath.Join(expandHome(dir), filename)
	}
	return filepath.Join(userHomeDir(), ".cmuxterm", filename)
}

func expandHome(path string) string {
	if path == "~" {
		return userHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (idx agentSessionIndex) recordFor(surfaceID string) (agentSessionRecord, bool) {
	if surfaceID == "" {
		return agentSessionRecord{}, false
	}
	record, ok := idx.bySurfaceID[surfaceID]
	return record, ok
}
