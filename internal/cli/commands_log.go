package cli

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/ops"
	"remork/internal/output"
)

func addLogCommand(root *cobra.Command, opts Options) {
	var limit int
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "log [flags]",
		Short: "Show recent remote Remork operations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			entries, err := runCtx.client.OperationsContext(cmd.Context(), runCtx.binding.RemoteRoot, limit)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), entries)
			}
			r := plainRenderer(cmd, false)
			r.Section("Operations")
			if len(entries) == 0 {
				r.Empty("no remote operations recorded", "run remork sync or remork status")
				return nil
			}
			r.Table([]string{"time", "client", "operation", "result", "summary"}, operationRows(entries))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of operations to show")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	root.AddCommand(cmd)
}

func operationRows(entries []ops.Entry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{
			operationTime(entry),
			entry.ClientID,
			displayOperation(entry.Operation),
			entry.Result,
			operationSummary(entry),
		})
	}
	return rows
}

func operationTime(entry ops.Entry) string {
	ts := entry.FinishedAt
	if ts.IsZero() {
		ts = entry.StartedAt
	}
	if ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format(time.RFC3339)
}

func displayOperation(operation string) string {
	if operation == "exec" {
		return "run"
	}
	return operation
}

func operationSummary(entry ops.Entry) string {
	if len(entry.Command) > 0 {
		return strings.Join(entry.Command, " ")
	}
	if len(entry.ChangedPaths) > 0 {
		return strings.Join(entry.ChangedPaths, ",")
	}
	if len(entry.RequestSummary) > 0 {
		data, err := json.Marshal(entry.RequestSummary)
		if err == nil {
			return string(data)
		}
	}
	if entry.ErrorMessage != "" {
		return entry.ErrorMessage
	}
	return "-"
}
