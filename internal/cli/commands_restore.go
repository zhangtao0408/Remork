package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"remork/internal/state"
	"remork/internal/transfer"
)

func addRestoreCommand(root *cobra.Command, opts Options) {
	var all bool

	cmd := &cobra.Command{
		Use:   "restore [path]",
		Short: "Discard local changes",
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return fmt.Errorf("restore --all does not accept a path")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("restore requires a path or --all")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadLocalChangeContext(opts)
			if err != nil {
				return err
			}
			changes, err := state.DetectDirty(ctx.localRoot, ctx.snapshot)
			if err != nil {
				return err
			}
			if !all {
				changes = filterChanges(changes, args[0])
			}
			for _, change := range changes {
				if err := restoreChange(ctx, change); err != nil {
					return err
				}
			}
			r := plainRenderer(cmd, false)
			r.Section("Restore complete")
			r.KeyValue("restored", len(changes))
			if len(changes) == 0 {
				r.Empty("no local changes matched", "run remork status")
				return nil
			}
			r.Success(fmt.Sprintf("restored %d", len(changes)))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Restore all dirty tracked paths")
	root.AddCommand(cmd)
}

func restoreChange(ctx localChangeContext, change state.DirtyChange) error {
	if change.Kind == state.ChangeCreate {
		localPath, err := transfer.LocalPath(ctx.localRoot, change.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := readBase(ctx.store, change.Path)
	if err != nil {
		return err
	}
	return transfer.WriteFile(ctx.localRoot, change.Path, data)
}
