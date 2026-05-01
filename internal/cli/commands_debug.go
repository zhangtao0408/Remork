package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/output"
	"remork/internal/watch"
)

func addDebugCommand(root *cobra.Command, opts Options) {
	debug := &cobra.Command{
		Use:   "debug",
		Short: "Inspect daemon APIs and events",
	}
	debug.AddCommand(newDebugManifestCommand(opts))
	debug.AddCommand(newDebugEventsCommand(opts))
	debug.AddCommand(newDebugAPICommand(opts))
	root.AddCommand(debug)
}

func newDebugManifestCommand(opts Options) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Inspect the remote manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				return err
			}
			manifest, err := runCtx.client.ManifestContext(cmd.Context(), runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), manifest)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "root: %s\n", manifest.Root)
			fmt.Fprintf(cmd.OutOrStdout(), "revision: %s\n", manifest.Revision)
			fmt.Fprintf(cmd.OutOrStdout(), "entries: %d\n", len(manifest.Entries))
			for _, entry := range manifest.Entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tlarge=%t\thash=%s\n", entry.Path, entry.Type, entry.Large, shortHash(entry.Hash))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	return cmd
}

func newDebugEventsCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Stream remote workspace events as JSON lines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return streamWorkspaceEvents(ctx, runCtx, nil, func(ev watch.Event) error {
				return writeEventJSONLine(cmd.OutOrStdout(), ev)
			}, nil, 0, 0, nil)
		},
	}
}

func newDebugAPICommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "Probe daemon APIs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				return err
			}
			var firstErr error
			ctx := cmd.Context()
			if err := printAPICheck(cmd, "status", func() (string, error) {
				status, err := runCtx.client.StatusContext(ctx)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("roots=%d version=%s", len(status.Roots), valueOrDash(status.Version)), nil
			}); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := printAPICheck(cmd, "manifest", func() (string, error) {
				manifest, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("entries=%d revision=%s", len(manifest.Entries), valueOrDash(manifest.Revision)), nil
			}); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := printAPICheck(cmd, "operations", func() (string, error) {
				entries, err := runCtx.client.OperationsContext(ctx, runCtx.binding.RemoteRoot, 5)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("entries=%d", len(entries)), nil
			}); err != nil && firstErr == nil {
				firstErr = err
			}
			return firstErr
		},
	}
}

func printAPICheck(cmd *cobra.Command, name string, call func() (string, error)) error {
	start := time.Now()
	result, err := call()
	latency := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "%s ERROR latency=%s result=%s\n", name, latency, err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s OK latency=%s result=%s\n", name, latency, result)
	return nil
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func shortHash(hash string) string {
	if hash == "" {
		return "-"
	}
	hash = strings.TrimSpace(hash)
	if len(hash) <= 19 {
		return hash
	}
	return hash[:19]
}
