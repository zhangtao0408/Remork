package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/tui"
)

func isAuthHTTPError(err error) bool {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == 401 || httpErr.StatusCode == 403
}

func updateHostTokenFile(homeDir string, host config.Host, token string) (config.Host, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return host, errors.New("token cannot be empty")
	}
	if host.TokenFile == "" {
		host.TokenFile = defaultConnectTokenFile(homeDir, host.Name)
	}
	if err := writeConnectTokenFile(host.TokenFile, token); err != nil {
		return host, err
	}
	host.TokenEnv = ""
	return host, nil
}

func retryAfterTokenFileUpdate(cmd *cobra.Command, opts Options, runCtx runContext, err error, retry func(runContext) error) error {
	if !isAuthHTTPError(err) {
		return err
	}
	if boolFlag(cmd, "non-interactive") || !commandHasPromptTTY(cmd) {
		return codedCommandError{code: exitcode.InvalidUsageOrConfig, err: err, fix: "run remork connect --url " + runCtx.host.URL + " to update the saved token"}
	}
	if runCtx.host.TokenEnv != "" {
		return codedCommandError{code: exitcode.InvalidUsageOrConfig, err: err, fix: "update " + runCtx.host.TokenEnv + " with the new daemon token"}
	}
	values, promptErr := runTUIForm(cmd, "Update daemon token", []tui.Field{
		{Section: "Auth", Key: "token", Label: "Token", Help: "Paste the current daemon token."},
	})
	if promptErr != nil {
		return promptErr
	}
	host, updateErr := updateHostTokenFile(opts.HomeDir, runCtx.host, values["token"])
	if updateErr != nil {
		return updateErr
	}
	store, storeErr := configStore(opts)
	if storeErr != nil {
		return storeErr
	}
	cfg, loadErr := store.LoadOrDefault()
	if loadErr != nil {
		return loadErr
	}
	cfg.Hosts[host.Name] = host
	if saveErr := store.Save(cfg); saveErr != nil {
		return saveErr
	}
	return retry(runCtx.withUpdatedHost(host))
}
