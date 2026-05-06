package cli

import (
	"os"
	"path/filepath"
	"sort"

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
			r := plainRenderer(cmd, false)
			r.Section("Workspace")
			r.KeyValue("local", localRoot)
			r.KeyValue("host", binding.Host)
			r.KeyValue("workspace root", binding.RemoteRoot)
			r.KeyValue("workspace_id", binding.WorkspaceID)
			r.KeyValue("state_scope", "local-checkout")
			r.KeyValue("state_dir", binding.StateDir)
			r.Command("remork status")
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
			r := plainRenderer(cmd, false)
			r.Section("Workspace removed")
			r.KeyValue("marker", marker)
			r.Success("removed workspace binding " + marker)
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
		plainRenderer(cmd, false).Empty("no workspaces registered", "run remork init HOST:/absolute/remote/workspace")
		return nil
	}
	ids := make([]string, 0, len(cfg.Workspaces))
	for id := range cfg.Workspaces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([][]string, 0, len(ids))
	for _, id := range ids {
		ws := cfg.Workspaces[id]
		rows = append(rows, []string{id, ws.Host, ws.RemoteRoot, ws.LocalRoot})
	}
	r := plainRenderer(cmd, false)
	r.Section("Workspaces")
	r.Table([]string{"id", "host", "workspace_root", "local_root"}, rows)
	return nil
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
