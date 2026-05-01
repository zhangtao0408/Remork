package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/syncer"
	"remork/internal/watch"
)

func addWatchCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Keep syncing from remote events",
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
			return watchEvents(ctx, cmd, runCtx)
		},
	}
	root.AddCommand(cmd)
}

func watchEvents(ctx context.Context, cmd *cobra.Command, runCtx runContext) error {
	var lastRevision string
	for {
		err := streamWorkspaceEvents(ctx, runCtx, func() error {
			if _, err := syncForWatch(ctx, cmd, runCtx, ""); err != nil {
				return err
			}
			manifest, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			lastRevision = manifest.Revision
			fmt.Fprintf(cmd.OutOrStdout(), "watching %s revision %s\n", runCtx.binding.RemoteRoot, lastRevision)
			return nil
		}, func(ev watch.Event) error {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", ev.Revision, ev.Kind, ev.Path)
			if _, err := syncForWatch(ctx, cmd, runCtx, watchSyncTarget(ev)); err != nil {
				return err
			}
			if needsWatchReconcile(ev) {
				manifest, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
				if err != nil {
					return err
				}
				lastRevision = manifest.Revision
				fmt.Fprintf(cmd.OutOrStdout(), "reconciled %s\n", lastRevision)
				return nil
			}
			if ev.Revision != "" {
				lastRevision = ev.Revision
			}
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "watch disconnected: %v; reconnecting\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func syncForWatch(ctx context.Context, cmd *cobra.Command, runCtx runContext, target string) (syncer.Result, error) {
	result, err := runCtx.runner.Sync(ctx, syncer.SyncOptions{TargetPath: target, Quiet: true})
	if err != nil {
		return result, err
	}
	if result.Downloaded > 0 || result.MetaWritten > 0 || result.Deleted > 0 || result.Conflicts > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "synced downloaded %d, meta %d, deleted %d, conflicts %d\n", result.Downloaded, result.MetaWritten, result.Deleted, result.Conflicts)
	}
	return result, nil
}

func watchSyncTarget(ev watch.Event) string {
	if needsWatchReconcile(ev) || ev.Kind == watch.EventDelete || ev.Kind == watch.EventRename {
		return ""
	}
	return ev.Path
}

func streamWorkspaceEvents(ctx context.Context, runCtx runContext, connected func() error, handle func(watch.Event) error) error {
	wsURL, err := buildEventsURL(runCtx.baseURL, runCtx.binding.RemoteRoot)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if runCtx.clientID != "" {
		headers.Set(api.HeaderClientID, runCtx.clientID)
	}
	if runCtx.token != "" {
		headers.Set("Authorization", "Bearer "+runCtx.token)
	}
	dialer := *websocket.DefaultDialer
	if runCtx.noProxy {
		dialer.Proxy = nil
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	if connected != nil {
		if err := connected(); err != nil {
			return err
		}
	}
	for {
		var ev watch.Event
		if err := conn.ReadJSON(&ev); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err := handle(ev); err != nil {
			return err
		}
	}
}

func buildEventsURL(baseURL, root string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/events"
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func needsWatchReconcile(ev watch.Event) bool {
	return ev.ResyncRequired || ev.Kind == watch.EventOverflow
}

func writeEventJSONLine(w interface{ Write([]byte) (int, error) }, ev watch.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
