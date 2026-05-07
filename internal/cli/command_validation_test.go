package cli

import "testing"

func TestValidateWorkspacePathArgAcceptsDotSlashRoot(t *testing.T) {
	if err := validateWorkspacePathArg("./"); err != nil {
		t.Fatalf("validateWorkspacePathArg(./) = %v, want nil", err)
	}
}
