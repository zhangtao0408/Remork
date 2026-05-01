package cli

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/prompt"
	"remork/internal/syncer"
	"remork/internal/workspace"
)

func addPullCommand(root *cobra.Command, opts Options) {
	var force bool
	var quiet bool
	var jsonOut bool
	var includeLarge bool

	cmd := &cobra.Command{
		Use:   "pull <path>",
		Short: "Fetch a specific file or directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, _, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				return fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
			}
			target, err := normalizePullTarget(args[0], binding)
			if err != nil {
				return err
			}
			runner, err := newBoundSyncRunner(opts)
			if err != nil {
				return err
			}
			result, err := runner.Pull(cmd.Context(), target, syncer.PullOptions{
				Force:        force,
				Quiet:        quiet,
				IncludeLarge: includeLarge,
				In:           cmd.InOrStdin(),
				Out:          cmd.ErrOrStderr(),
			})
			if err != nil {
				if errors.Is(err, prompt.ErrPromptRequired) {
					return codedCommandError{code: exitcode.PromptRequired, err: err}
				}
				return err
			}
			if result.Conflicts > 0 {
				return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("pull completed with %d conflicts", result.Conflicts)}
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), result)
			}
			if !quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "pull complete: downloaded %d, meta %d, skipped %d, conflicts %d\n", result.Downloaded, result.MetaWritten, result.Skipped, result.Conflicts)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm large-file downloads and overwrite dirty local files")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress text summary")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&includeLarge, "include-large", false, "Download large files instead of placeholders")
	_ = cmd.Flags().MarkHidden("include-large")
	root.AddCommand(cmd)
}

func normalizePullTarget(target string, binding workspace.Binding) (string, error) {
	if !strings.Contains(target, ":/") {
		return target, nil
	}
	host, remotePath, err := config.ParseWorkspaceRef(target)
	if err != nil {
		return "", err
	}
	if host != binding.Host {
		return "", fmt.Errorf("pull target host %q does not match bound host %q", host, binding.Host)
	}
	root := path.Clean(binding.RemoteRoot)
	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	remotePath = path.Clean(remotePath)
	if root == "/" {
		rel := strings.TrimPrefix(remotePath, "/")
		if rel == "" {
			return ".", nil
		}
		return rel, nil
	}
	if remotePath == root {
		return ".", nil
	}
	prefix := strings.TrimRight(root, "/") + "/"
	if !strings.HasPrefix(remotePath, prefix) {
		return "", fmt.Errorf("pull target %q is outside bound remote root %q", remotePath, root)
	}
	return strings.TrimPrefix(remotePath, prefix), nil
}
