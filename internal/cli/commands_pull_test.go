package cli

import (
	"strings"
	"testing"

	"remork/internal/workspace"
)

func TestNormalizePullTargetAcceptsMetaPullCommandTarget(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	got, err := normalizePullTarget("lab:/remote/root/checkpoints/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "checkpoints/model.tar.gz" {
		t.Fatalf("target = %q, want checkpoints/model.tar.gz", got)
	}
}

func TestNormalizePullTargetLeavesRelativeTargetUnchanged(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	got, err := normalizePullTarget("checkpoints/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "checkpoints/model.tar.gz" {
		t.Fatalf("target = %q, want checkpoints/model.tar.gz", got)
	}
}

func TestNormalizePullTargetRejectsOtherWorkspaceRefs(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	_, err := normalizePullTarget("other:/remote/root/model.tar.gz", binding)
	if err == nil || !strings.Contains(err.Error(), "does not match bound host") {
		t.Fatalf("err = %v, want host mismatch", err)
	}

	_, err = normalizePullTarget("lab:/remote/root-sibling/model.tar.gz", binding)
	if err == nil || !strings.Contains(err.Error(), "outside bound remote root") {
		t.Fatalf("err = %v, want root mismatch", err)
	}
}

func TestNormalizePullTargetHandlesRootWorkspace(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/"}
	got, err := normalizePullTarget("lab:/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "model.tar.gz" {
		t.Fatalf("target = %q, want model.tar.gz", got)
	}
}
