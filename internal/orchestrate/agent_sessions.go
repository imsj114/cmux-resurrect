package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/drolosoft/cmux-resurrect/internal/model"
)

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
	idx := agentSessionIndex{bySurfaceID: make(map[string]agentSessionRecord)}
	loadCodexHookSessions(&idx)
	return idx
}

func loadCodexHookSessions(idx *agentSessionIndex) {
	path := cmuxAgentHookStatePath("codex")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
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
