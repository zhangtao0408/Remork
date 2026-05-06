package cli

import (
	"strings"
	"testing"
)

func TestSetupScopeDoesNotAssumeCurrentProject(t *testing.T) {
	items := setupScopeItems(false)
	if len(items) == 0 || items[0].Name != "Connect this project" {
		t.Fatalf("first setup scope = %#v", items)
	}
	foundPrepare := false
	for _, item := range items {
		if item.Name == "Only prepare a server" {
			foundPrepare = true
		}
	}
	if !foundPrepare {
		t.Fatalf("setup scopes should include server-only option: %#v", items)
	}
}

func TestSetupScopeItemsForBoundWorkspacePreferUpdate(t *testing.T) {
	items := setupScopeItems(true)
	if len(items) == 0 || !strings.Contains(items[0].Name, "Update") {
		t.Fatalf("bound setup should prefer update/repair, got %#v", items)
	}
}
