package cli

import (
	"strings"
	"testing"
)

func TestDaemonDeploySpecBuildsPlanForInstall(t *testing.T) {
	spec := DaemonDeploySpec{
		Action:    "install",
		HostName:  "lab",
		SSHTarget: "lab.example",
		Roots:     []string{"/data/project"},
		Addr:      "127.0.0.1:17731",
		LocalBin:  fakeDaemonBinary(t),
		RemoteBin: ".local/bin/remorkd",
		URL:       "http://lab.example:17731",
		TokenEnv:  "",
		NoProxy:   true,
		Verify:    true,
		Execute:   true,
		Confirmed: true,
	}

	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		t.Fatalf("BuildDaemonDeployPlan: %v", err)
	}
	if plan.Title != "Daemon install" {
		t.Fatalf("title = %q, want Daemon install", plan.Title)
	}
	for _, want := range []string{
		"prepare remote directories",
		"stop existing remorkd daemon",
		"copy remorkd binary",
		"mark remorkd executable",
		"start remorkd daemon",
		"save host config",
		"verify daemon status",
	} {
		if !plan.HasAction(want) {
			t.Fatalf("plan actions missing %q: %#v", want, plan.Actions)
		}
	}
	if got := strings.Join(plan.Commands, "\n"); !strings.Contains(got, "scp") || !strings.Contains(got, ".local/bin/remorkd") {
		t.Fatalf("commands should include scp to remote bin, got:\n%s", got)
	}
}

func TestDaemonDeploySpecReusesNetworkBindValidation(t *testing.T) {
	_, err := BuildDaemonDeployPlan(DaemonDeploySpec{
		Action:    "install",
		HostName:  "lab",
		SSHTarget: "lab.example",
		Roots:     []string{"/data"},
		Addr:      "0.0.0.0:17731",
		LocalBin:  fakeDaemonBinary(t),
		RemoteBin: ".local/bin/remorkd",
		Execute:   true,
		Confirmed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "without authentication") {
		t.Fatalf("BuildDaemonDeployPlan error = %v, want unauthenticated network bind validation", err)
	}
}
