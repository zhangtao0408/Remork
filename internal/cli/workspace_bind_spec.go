package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type WorkspaceBindSpec struct {
	HostName   string
	RemoteRoot string
	LocalRoot  string
}

func PlanWorkspaceBind(opts Options, spec WorkspaceBindSpec) (OperationPlan, error) {
	_ = opts
	if spec.HostName == "" || spec.RemoteRoot == "" {
		return OperationPlan{}, fmt.Errorf("host and workspace root are required")
	}
	return OperationPlan{
		Title: "Bind workspace",
		Target: map[string]string{
			"host":           spec.HostName,
			"workspace root": spec.RemoteRoot,
			"local root":     spec.LocalRoot,
		},
		Actions: []PlannedAction{
			{Label: "verify daemon status"},
			{Label: "verify workspace root"},
			{Label: "write workspace binding"},
		},
		Next: []string{"remork sync"},
	}, nil
}

func ExecuteWorkspaceBindSpec(opts Options, spec WorkspaceBindSpec) error {
	if _, err := PlanWorkspaceBind(opts, spec); err != nil {
		return err
	}
	workingDir := opts.WorkingDir
	if spec.LocalRoot != "" {
		opts.WorkingDir = spec.LocalRoot
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = workingDir
	}
	return initWorkspace(specCommand(), opts, spec.HostName, spec.RemoteRoot)
}

func specCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "workspace-bind-spec"}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}
