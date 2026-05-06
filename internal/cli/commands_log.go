package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
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
				return err
			}
			entries, err := runCtx.client.OperationsContext(cmd.Context(), runCtx.binding.RemoteRoot, limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), entries)
			}
			plainRenderer(cmd, false).Section("Operations")
			printOperationTable(cmd.OutOrStdout(), entries)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of operations to show")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	root.AddCommand(cmd)
}

func printOperationTable(w interface{ Write([]byte) (int, error) }, entries []ops.Entry) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "time\tclient\toperation\tresult\tsummary")
	for _, entry := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			operationTime(entry),
			entry.ClientID,
			displayOperation(entry.Operation),
			entry.Result,
			operationSummary(entry),
		)
	}
	_ = tw.Flush()
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
