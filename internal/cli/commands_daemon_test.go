package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDaemonDeployPlanWarnsForNonLoopbackNoToken(t *testing.T) {
	var out bytes.Buffer
	printDaemonDeployPlan(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab-a",
		root:      "/data/project-a",
		addr:      "0.0.0.0:17731",
		localBin:  "dist/remorkd-linux-arm64",
		remoteBin: "/tmp/remorkd",
	})

	got := out.String()
	for _, want := range []string{"WARNING", "remote command execution", "--token-file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("deploy plan should contain %q, got:\n%s", want, got)
		}
	}
}

func TestInsecureNoTokenNonLoopbackAddrCases(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		hasToken bool
		want     bool
	}{
		{name: "wildcard ipv4", addr: "0.0.0.0:17731", want: true},
		{name: "wildcard ipv6", addr: "[::]:17731", want: true},
		{name: "expanded wildcard ipv6", addr: "[0:0:0:0:0:0:0:0]:17731", want: true},
		{name: "empty host wildcard", addr: ":17731", want: true},
		{name: "loopback ipv4", addr: "127.0.0.1:17731"},
		{name: "localhost", addr: "localhost:17731"},
		{name: "loopback ipv6", addr: "[::1]:17731"},
		{name: "wildcard with token", addr: "0.0.0.0:17731", hasToken: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := insecureNoTokenNonLoopbackAddr(tt.addr, tt.hasToken); got != tt.want {
				t.Fatalf("insecureNoTokenNonLoopbackAddr(%q, %t) = %t, want %t", tt.addr, tt.hasToken, got, tt.want)
			}
		})
	}
}
