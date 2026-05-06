package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/workspace"
)

func addDoctorCommand(root *cobra.Command, opts Options) {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local and remote readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				return runDoctorJSON(cmd, opts)
			}
			r := plainRenderer(cmd, false)
			r.Section("Doctor")
			warnings, err := runDoctor(cmd.Context(), opts)
			if err != nil {
				r.Error("FAILED: "+err.reason, "Fix: "+err.fix)
				return silentCommandError{err: codedCommandError{code: err.code, err: errors.New(err.reason), fix: err.fix}}
			}
			r.Success("OK: workspace is ready")
			for _, warning := range warnings {
				r.Warning("WARN: " + warning)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	root.AddCommand(cmd)
}

type doctorJSONResult struct {
	Ready    bool     `json:"ready"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
	Fix      string   `json:"fix,omitempty"`
	Code     int      `json:"code,omitempty"`
}

func runDoctorJSON(cmd *cobra.Command, opts Options) error {
	warnings, failure := runDoctor(cmd.Context(), opts)
	result := doctorJSONResult{Ready: failure == nil, Warnings: warnings}
	if failure != nil {
		result.Error = failure.reason
		result.Fix = failure.fix
		result.Code = failure.code
		if err := output.WriteJSON(cmd.OutOrStdout(), result); err != nil {
			return err
		}
		return silentCommandError{err: codedCommandError{code: failure.code, err: errors.New(failure.reason)}}
	}
	return output.WriteJSON(cmd.OutOrStdout(), result)
}

type doctorFailure struct {
	reason string
	fix    string
	code   int
}

func runDoctor(ctx context.Context, opts Options) ([]string, *doctorFailure) {
	if ctx == nil {
		ctx = context.Background()
	}
	store, err := configStore(opts)
	if err != nil {
		return nil, configDoctorFailure(err, "set HOME or pass a valid remork home directory")
	}
	cfg, err := store.Load()
	if err != nil {
		return nil, configDoctorFailure(err, "run remork host add NAME --url URL, then run remork init HOST:/absolute/remote/path in your local project directory")
	}
	binding, _, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return nil, &doctorFailure{
			reason: "current directory is not bound to a remork workspace",
			fix:    "run remork init HOST:/absolute/remote/path",
			code:   exitcode.InvalidUsageOrConfig,
		}
	}
	host, ok := cfg.Hosts[binding.Host]
	if !ok {
		return nil, &doctorFailure{
			reason: fmt.Sprintf("host %q is not configured", binding.Host),
			fix:    fmt.Sprintf("run remork host add %s --url URL", binding.Host),
			code:   exitcode.InvalidUsageOrConfig,
		}
	}
	_, err = auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return nil, &doctorFailure{
			reason: err.Error(),
			fix:    fmt.Sprintf("export %s=<token>", host.TokenEnv),
			code:   exitcode.InvalidUsageOrConfig,
		}
	}
	var warnings []string
	if host.TokenEnv == "" {
		warnings = append(warnings, "host has no token configured; use only on trusted VPN/private networks")
	}
	status, err := opts.DaemonProbe.Status(ctx, host, cfg.ClientID)
	if err != nil {
		return nil, networkDoctorFailure(err, "start remorkd and check remork host add URL")
	}
	ok, err = remoteRootAdvertised(status.Roots, binding.RemoteRoot)
	if err != nil {
		return nil, configDoctorFailure(err, "check remorkd advertised allowed roots")
	}
	if !ok {
		return nil, &doctorFailure{
			reason: fmt.Sprintf("remote workspace %q is outside advertised allowed roots", binding.RemoteRoot),
			fix:    "restart remorkd with an allowed root containing " + binding.RemoteRoot,
			code:   exitcode.InvalidUsageOrConfig,
		}
	}
	if _, err := opts.DaemonProbe.Manifest(ctx, host, cfg, binding.RemoteRoot); err != nil {
		return nil, networkDoctorFailure(err, "check remote root permissions and remorkd manifest access")
	}
	if _, err := opts.DaemonProbe.Operations(ctx, host, cfg, binding.RemoteRoot, 1); err != nil {
		return nil, networkDoctorFailure(err, "check remorkd /operations access for this workspace")
	}
	return warnings, nil
}

func configDoctorFailure(err error, fix string) *doctorFailure {
	reason := err.Error()
	if errors.Is(err, os.ErrNotExist) {
		reason = "remork has not been configured on this machine"
	}
	return &doctorFailure{reason: reason, fix: fix, code: exitcode.InvalidUsageOrConfig}
}

func networkDoctorFailure(err error, fix string) *doctorFailure {
	code := exitcode.NetworkUnavailable
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 {
			code = exitcode.PermissionDenied
		}
	}
	return &doctorFailure{reason: explainDaemonStatusError(err), fix: fix, code: code}
}
