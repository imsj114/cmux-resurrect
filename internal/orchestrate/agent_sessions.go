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

var (
	remoteCodexHookSessionReader    = readRemoteCodexHookSessionStore
	remoteCodexProcessSessionReader = readRemoteCodexProcessSessions
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
	if err == nil {
		loadCodexHookSessionData(idx, data)
	}
	data, err = remoteCodexProcessSessionReader(remote)
	if err == nil {
		loadCodexProcessSessionData(idx, data)
	}
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
		idx.setRecord(surfaceID, next)
	}
}

func loadCodexProcessSessionData(idx *agentSessionIndex, data []byte) {
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		surfaceID := strings.TrimSpace(fields[0])
		sessionID := strings.TrimSpace(fields[1])
		if surfaceID == "" || sessionID == "" {
			continue
		}
		record := agentSessionRecord{
			agent: model.AgentSession{Kind: "codex", SessionID: sessionID},
		}
		if len(fields) >= 3 {
			record.cwd = strings.TrimSpace(fields[2])
		}
		if len(fields) >= 4 {
			if updatedAt, err := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64); err == nil {
				record.updatedAt = updatedAt
			}
		}
		idx.setRecord(surfaceID, record)
	}
}

func (idx agentSessionIndex) setRecord(surfaceID string, record agentSessionRecord) {
	key := agentSessionKey(surfaceID)
	if key == "" {
		return
	}
	if existing, ok := idx.bySurfaceID[key]; ok && existing.updatedAt > record.updatedAt {
		return
	}
	idx.bySurfaceID[key] = record
}

func readRemoteCodexHookSessionStore(remote *model.RemoteWorkspace) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := remoteCodexHookSessionSSHArgs(remote)
	return exec.CommandContext(ctx, "ssh", args...).Output()
}

func remoteCodexHookSessionSSHArgs(remote *model.RemoteWorkspace) []string {
	script := `p="${CMUX_AGENT_HOOK_STATE_DIR:-$HOME/.cmuxterm}/codex-hook-sessions.json"; if [ -r "$p" ]; then cat "$p"; fi`
	return remoteSSHCommandArgs(remote, script)
}

func readRemoteCodexProcessSessions(remote *model.RemoteWorkspace) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := remoteCodexProcessSessionSSHArgs(remote)
	return exec.CommandContext(ctx, "ssh", args...).Output()
}

func remoteCodexProcessSessionSSHArgs(remote *model.RemoteWorkspace) []string {
	script := `for envfile in /proc/[0-9]*/environ; do
  [ -r "$envfile" ] || continue
  pid="${envfile#/proc/}"
  pid="${pid%/environ}"
  cmd="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
  case "$cmd" in
    *codex*) ;;
    *) continue ;;
  esac
  env="$(tr '\000' '\n' < "$envfile" 2>/dev/null || true)"
  surface="$(printf '%s\n' "$env" | sed -n 's/^CMUX_SURFACE_ID=//p' | head -n 1)"
  [ -n "$surface" ] || continue
  cwd="$(readlink "/proc/$pid/cwd" 2>/dev/null || printf '%s\n' "$env" | sed -n 's/^PWD=//p' | head -n 1)"
  for fd in /proc/$pid/fd/*; do
    target="$(readlink "$fd" 2>/dev/null || true)"
    case "$target" in
      */.codex/sessions/*rollout-*.jsonl)
        base="${target##*/}"
        session="$(printf '%s\n' "$base" | grep -Eo '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | tail -n 1)"
        [ -n "$session" ] || continue
        updated="$(stat -c %Y "$target" 2>/dev/null || printf 0)"
        printf '%s\t%s\t%s\t%s\n' "$surface" "$session" "$cwd" "$updated"
        break
        ;;
    esac
  done
done`
	return remoteSSHCommandArgs(remote, script)
}

func remoteSSHCommandArgs(remote *model.RemoteWorkspace, script string) []string {
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
	key := agentSessionKey(surfaceID)
	if key == "" {
		return agentSessionRecord{}, false
	}
	record, ok := idx.bySurfaceID[key]
	return record, ok
}

func agentSessionKey(surfaceID string) string {
	return strings.ToUpper(strings.TrimSpace(surfaceID))
}
