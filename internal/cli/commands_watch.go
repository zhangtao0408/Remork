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
	watchOpts := defaultWatchOptions()
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
			return watchEvents(ctx, cmd, runCtx, watchOpts)
		},
	}
	cmd.Flags().DurationVar(&watchOpts.ReconcileInterval, "reconcile-interval", watchOpts.ReconcileInterval, "Periodically run a full reconcile while connected")
	cmd.Flags().DurationVar(&watchOpts.Debounce, "debounce", watchOpts.Debounce, "Debounce rapid watch events before syncing")
	root.AddCommand(cmd)
}

type watchOptions struct {
	ReconcileInterval time.Duration
	Debounce          time.Duration
}

func defaultWatchOptions() watchOptions {
	return watchOptions{
		ReconcileInterval: 30 * time.Second,
		Debounce:          200 * time.Millisecond,
	}
}

func watchEvents(ctx context.Context, cmd *cobra.Command, runCtx runContext, opts watchOptions) error {
	var lastRevision string
	for {
		handle, flush := newWatchEventAccumulator(ctx, cmd, runCtx, &lastRevision)
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
		}, handle, flush, opts.Debounce, opts.ReconcileInterval, func() error {
			if _, err := syncForWatch(ctx, cmd, runCtx, ""); err != nil {
				return err
			}
			manifest, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			lastRevision = manifest.Revision
			fmt.Fprintf(cmd.OutOrStdout(), "reconciled %s\n", lastRevision)
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

func newWatchEventAccumulator(ctx context.Context, cmd *cobra.Command, runCtx runContext, lastRevision *string) (func(watch.Event) error, func() error) {
	var pending *pendingWatchSync
	handle := func(ev watch.Event) error {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", ev.Revision, ev.Kind, ev.Path)
		target := watchSyncTarget(ev)
		needsReconcile := target == ""
		if pending == nil {
			pending = &pendingWatchSync{target: target, revision: ev.Revision, needsReconcile: needsReconcile}
		} else {
			if pending.target != target {
				pending.target = ""
				pending.needsReconcile = true
			}
			if needsReconcile {
				pending.target = ""
				pending.needsReconcile = true
			}
			if ev.Revision != "" {
				pending.revision = ev.Revision
			}
		}
		return nil
	}
	flush := func() error {
		if pending == nil {
			return nil
		}
		current := *pending
		pending = nil
		if _, err := syncForWatch(ctx, cmd, runCtx, current.target); err != nil {
			return err
		}
		if current.needsReconcile {
			manifest, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			*lastRevision = manifest.Revision
			fmt.Fprintf(cmd.OutOrStdout(), "reconciled %s\n", *lastRevision)
			return nil
		}
		if current.revision != "" {
			*lastRevision = current.revision
		}
		return nil
	}
	return handle, flush
}

type pendingWatchSync struct {
	target         string
	revision       string
	needsReconcile bool
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

func streamWorkspaceEvents(ctx context.Context, runCtx runContext, connected func() error, handle func(watch.Event) error, flush func() error, debounce time.Duration, tickInterval time.Duration, tick func() error) error {
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
	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-streamDone:
		}
	}()
	defer close(streamDone)
	if connected != nil {
		if err := connected(); err != nil {
			return err
		}
	}
	events := make(chan watch.Event, 1)
	readErr := make(chan error, 1)
	go func() {
		for {
			var ev watch.Event
			if err := conn.ReadJSON(&ev); err != nil {
				select {
				case readErr <- err:
				case <-streamDone:
				case <-ctx.Done():
				}
				return
			}
			select {
			case events <- ev:
			case <-streamDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	var ticker *time.Ticker
	var tickC <-chan time.Time
	if tickInterval > 0 && tick != nil {
		ticker = time.NewTicker(tickInterval)
		tickC = ticker.C
		defer ticker.Stop()
	}
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	stopDebounce := func() {
		if debounceTimer == nil {
			return
		}
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceTimer = nil
		debounceC = nil
	}
	resetDebounce := func() error {
		stopDebounce()
		debounceTimer = time.NewTimer(debounce)
		debounceC = debounceTimer.C
		return nil
	}
	flushPending := func() error {
		stopDebounce()
		if flush != nil {
			return flush()
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if flushErr := flushPending(); flushErr != nil {
				return flushErr
			}
			return err
		case <-tickC:
			if err := flushPending(); err != nil {
				return err
			}
			if err := tick(); err != nil {
				return err
			}
		case <-debounceC:
			if err := flushPending(); err != nil {
				return err
			}
		case ev := <-events:
			if err := handle(ev); err != nil {
				return err
			}
			if debounce > 0 {
				if err := resetDebounce(); err != nil {
					return err
				}
			} else if err := flushPending(); err != nil {
				return err
			}
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
