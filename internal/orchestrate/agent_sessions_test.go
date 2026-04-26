package orchestrate

import (
	"strings"
	"testing"

	"github.com/drolosoft/cmux-resurrect/internal/model"
)

func TestRemoteCodexHookSessionSSHArgsUseSingleRemoteCommand(t *testing.T) {
	args := remoteCodexHookSessionSSHArgs(&model.RemoteWorkspace{
		Destination:  "home",
		Port:         2222,
		IdentityFile: "~/.ssh/id_ed25519",
		SSHOptions:   []string{"ProxyJump jump"},
	})

	if len(args) < 3 {
		t.Fatalf("args too short: %#v", args)
	}
	if args[len(args)-3] != "--" || args[len(args)-2] != "home" {
		t.Fatalf("destination tail = %#v, want -- home <command>", args)
	}
	command := args[len(args)-1]
	if !strings.HasPrefix(command, "sh -lc ") {
		t.Fatalf("remote command = %q, want sh -lc command string", command)
	}
	if !strings.Contains(command, "codex-hook-sessions.json") {
		t.Fatalf("remote command should read codex hook state: %q", command)
	}
	for _, arg := range args[len(args)-1:] {
		if arg == "sh" || arg == "-lc" {
			t.Fatalf("remote command must not be split across ssh args: %#v", args)
		}
	}
}

func TestRemoteCodexProcessSessionSSHArgsUseSingleRemoteCommand(t *testing.T) {
	args := remoteCodexProcessSessionSSHArgs(&model.RemoteWorkspace{Destination: "home"})
	if len(args) < 3 {
		t.Fatalf("args too short: %#v", args)
	}
	if args[len(args)-3] != "--" || args[len(args)-2] != "home" {
		t.Fatalf("destination tail = %#v, want -- home <command>", args)
	}
	command := args[len(args)-1]
	if !strings.HasPrefix(command, "sh -lc ") {
		t.Fatalf("remote command = %q, want sh -lc command string", command)
	}
	if !strings.Contains(command, "/proc/[0-9]*/environ") || !strings.Contains(command, ".codex/sessions") {
		t.Fatalf("remote command should inspect codex process sessions: %q", command)
	}
}

func TestLoadCodexProcessSessionData(t *testing.T) {
	idx := newAgentSessionIndex()
	loadCodexProcessSessionData(&idx, []byte("BEAD9FEB-470F-4BBA-8FEA-2AC91C9FE44C\t019dca4e-e022-7f03-aa12-e02a1c102d56\t/home/ubuntu/bin\t1777215690\n"))

	record, ok := idx.recordFor("bead9feb-470f-4bba-8fea-2ac91c9fe44c")
	if !ok {
		t.Fatal("missing process-derived agent session")
	}
	if record.agent.Kind != "codex" || record.agent.SessionID != "019dca4e-e022-7f03-aa12-e02a1c102d56" {
		t.Errorf("agent = %#v, want codex session", record.agent)
	}
	if record.cwd != "/home/ubuntu/bin" {
		t.Errorf("cwd = %q, want /home/ubuntu/bin", record.cwd)
	}
}
