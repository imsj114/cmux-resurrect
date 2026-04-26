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
