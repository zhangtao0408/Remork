//go:build !windows

package shellclient

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"remork/internal/api"
)

func watchResize(ctx context.Context, in io.Reader, conn *websocket.Conn, writeMu *sync.Mutex) func() {
	f, ok := in.(*os.File)
	if !ok {
		return func() {}
	}
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-signals:
				rows, cols, err := pty.Getsize(f)
				if err == nil && rows > 0 && cols > 0 {
					_ = writeJSON(writeMu, conn, api.ShellFrame{Type: "resize", Rows: rows, Cols: cols})
				}
			}
		}
	}()
	return func() { close(done) }
}
