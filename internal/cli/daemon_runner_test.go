package cli

import (
	"runtime"
	"strings"
	"testing"
)

func TestSSHCommandArgsBoundConnectionAndPasswordPrompts(t *testing.T) {
	args := sshCommandArgs("lab.example", "uname -s")
	got := strings.Join(args, " ")
	for _, want := range []string{
		"ConnectTimeout=10",
		"ServerAliveInterval=5",
		"ServerAliveCountMax=2",
		"NumberOfPasswordPrompts=3",
		"lab.example",
		"uname -s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ssh args should contain %q, got %#v", want, args)
		}
	}
	if runtime.GOOS != "windows" && !strings.Contains(got, "ControlMaster=auto") {
		t.Fatalf("ssh args should enable connection reuse on platforms that support ControlMaster, got %#v", args)
	}
	if runtime.GOOS != "windows" && !strings.Contains(got, "/tmp/rmkssh-") {
		t.Fatalf("ssh control path should use a short path, got %#v", args)
	}
}

func TestSCPCommandArgsUseSameSSHOptions(t *testing.T) {
	args := scpCommandArgs("local-remorkd", "lab.example:.local/bin/remorkd")
	got := strings.Join(args, " ")
	for _, want := range []string{"ConnectTimeout=10", "NumberOfPasswordPrompts=3", "local-remorkd", "lab.example:.local/bin/remorkd"} {
		if !strings.Contains(got, want) {
			t.Fatalf("scp args should contain %q, got %#v", want, args)
		}
	}
}
