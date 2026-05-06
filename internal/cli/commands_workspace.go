package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/output"
	"remork/internal/workspace"
)

func addWorkspaceCommand(root *cobra.Command, opts Options) {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Inspect or remove local bindings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, unboundWorkspaceError(err))
				}
				return unboundWorkspaceError(err)
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), workspaceInfoJSON{
					LocalRoot:   localRoot,
					Host:        binding.Host,
					RemoteRoot:  binding.RemoteRoot,
					WorkspaceID: binding.WorkspaceID,
					StateDir:    binding.StateDir,
				})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "local: %s\n", localRoot)
			fmt.Fprintf(out, "host: %s\n", binding.Host)
			fmt.Fprintf(out, "remote_root: %s\n", binding.RemoteRoot)
			fmt.Fprintf(out, "workspace_id: %s\n", binding.WorkspaceID)
			fmt.Fprintf(out, "state_scope: local-checkout\n")
			fmt.Fprintf(out, "state_dir: %s\n", binding.StateDir)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	var listJSON bool
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove the local workspace binding marker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				return unboundWorkspaceError(err)
			}
			marker := filepath.Join(localRoot, workspace.MarkerName)
			if err := os.Remove(marker); err != nil {
				return err
			}
			if err := removeWorkspaceRegistryEntry(opts, binding.WorkspaceID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed workspace binding %s\n", marker)
			return nil
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List registered local workspace bindings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listWorkspaces(cmd, opts, listJSON)
		},
	}
	list.Flags().BoolVar(&listJSON, "json", false, "Print JSON output")
	cmd.AddCommand(list)
	cmd.AddCommand(remove)
	root.AddCommand(cmd)
}

type workspaceInfoJSON struct {
	LocalRoot   string `json:"local_root"`
	Host        string `json:"host"`
	RemoteRoot  string `json:"remote_root"`
	WorkspaceID string `json:"workspace_id"`
	StateDir    string `json:"state_dir"`
}

func listWorkspaces(cmd *cobra.Command, opts Options, jsonOut bool) error {
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	if jsonOut {
		return output.WriteJSON(cmd.OutOrStdout(), struct {
			Workspaces map[string]config.Workspace `json:"workspaces"`
		}{Workspaces: cfg.Workspaces})
	}
	if len(cfg.Workspaces) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no workspaces registered")
		return nil
	}
	ids := make([]string, 0, len(cfg.Workspaces))
	for id := range cfg.Workspaces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\thost\tremote_root\tlocal_root")
	for _, id := range ids {
		ws := cfg.Workspaces[id]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", id, ws.Host, ws.RemoteRoot, ws.LocalRoot)
	}
	return tw.Flush()
}

func removeWorkspaceRegistryEntry(opts Options, id string) error {
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	delete(cfg.Workspaces, id)
	return store.Save(cfg)
}
