package shellclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"remork/internal/api"
)

type Options struct {
	BaseURL  string
	Root     string
	ClientID string
	Token    string
	Stdin    io.Reader
	Stdout   io.Writer
	Rows     int
	Cols     int
	Dialer   *websocket.Dialer
}

func BuildURL(baseURL, root string) (string, error) {
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
	u.Path = strings.TrimRight(u.Path, "/") + "/shell"
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func Run(ctx context.Context, opts Options) error {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	dialer := opts.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	wsURL, err := BuildURL(opts.BaseURL, opts.Root)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if opts.ClientID != "" {
		headers.Set(api.HeaderClientID, opts.ClientID)
	}
	if opts.Token != "" {
		headers.Set("Authorization", "Bearer "+opts.Token)
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, cols := initialSize(opts)
	var writeMu sync.Mutex
	if err := writeJSON(&writeMu, conn, api.ShellFrame{Type: "resize", Rows: rows, Cols: cols}); err != nil {
		return err
	}
	stopResize := watchResize(ctx, opts.Stdin, conn, &writeMu)
	defer stopResize()

	type copyResult struct {
		stream string
		err    error
	}
	errCh := make(chan copyResult, 2)
	go func() {
		errCh <- copyResult{stream: "input", err: copyInput(ctx, opts.Stdin, conn, &writeMu)}
	}()
	go func() {
		errCh <- copyResult{stream: "output", err: copyOutput(opts.Stdout, conn)}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		case result := <-errCh:
			if result.stream == "input" && result.err == nil {
				continue
			}
			if result.err == nil || isSocketClosed(result.err) {
				return nil
			}
			return result.err
		}
	}
}

func initialSize(opts Options) (int, int) {
	rows, cols := opts.Rows, opts.Cols
	if rows > 0 && cols > 0 {
		return rows, cols
	}
	if f, ok := opts.Stdin.(*os.File); ok {
		if gotRows, gotCols, err := pty.Getsize(f); err == nil && gotRows > 0 && gotCols > 0 {
			if rows == 0 {
				rows = gotRows
			}
			if cols == 0 {
				cols = gotCols
			}
		}
	}
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	return rows, cols
}

func copyInput(ctx context.Context, in io.Reader, conn *websocket.Conn, writeMu *sync.Mutex) error {
	buf := make([]byte, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			if writeErr := writeMessage(writeMu, conn, websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if writeErr := writeMessage(writeMu, conn, websocket.BinaryMessage, []byte{4}); writeErr != nil {
					return writeErr
				}
				return nil
			}
			return err
		}
	}
}

func copyOutput(out io.Writer, conn *websocket.Conn) error {
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			if _, err := out.Write(msg); err != nil {
				return err
			}
		}
	}
}

func writeJSON(writeMu *sync.Mutex, conn *websocket.Conn, frame api.ShellFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return writeMessage(writeMu, conn, websocket.TextMessage, data)
}

func writeMessage(writeMu *sync.Mutex, conn *websocket.Conn, typ int, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteMessage(typ, data)
}

func isSocketClosed(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived, websocket.CloseAbnormalClosure)
}
