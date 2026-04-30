//go:build windows

package shellclient

import (
	"context"
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

func watchResize(ctx context.Context, in io.Reader, conn *websocket.Conn, writeMu *sync.Mutex) func() {
	return func() {}
}
