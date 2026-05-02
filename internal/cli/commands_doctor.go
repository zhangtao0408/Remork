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
	"remork/internal/workspace"
)

func addDoctorCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local and remote readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			warnings, err := runDoctor(cmd.Context(), opts)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "FAILED: %s\n", err.reason)
				fmt.Fprintf(cmd.OutOrStdout(), "Fix: %s\n", err.fix)
				return codedCommandError{code: err.code, err: errors.New(err.reason)}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "OK: workspace is ready")
			for _, warning := range warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "WARN: %s\n", warning)
			}
			return nil
		},
	}
	root.AddCommand(cmd)
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
		return nil, configDoctorFailure(err, "run remork host add NAME --url URL")
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
		reason = "config file is not readable"
	}
	return &doctorFailure{reason: reason, fix: fix, code: exitcode.InvalidUsageOrConfig}
}

func networkDoctorFailure(err error, fix string) *doctorFailure {
	code := exitcode.NetworkUnavailable
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == 403 {
		code = exitcode.PermissionDenied
	}
	return &doctorFailure{reason: err.Error(), fix: fix, code: code}
}
