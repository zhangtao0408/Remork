package shellclient

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/api"
)

func TestBuildShellURLIncludesRootAndUsesWebSocketScheme(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "http uses ws",
			base: "http://127.0.0.1:17731",
			want: "ws://127.0.0.1:17731/shell?root=%2Fdata%2Fproject",
		},
		{
			name: "https uses wss",
			base: "https://remork.example",
			want: "wss://remork.example/shell?root=%2Fdata%2Fproject",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(tt.base, "/data/project")
			if err != nil {
				t.Fatalf("build url: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewDialerNoProxyDisablesProxyFromEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	dialer := NewDialer(true)
	if dialer.Proxy != nil {
		req, err := http.NewRequest(http.MethodGet, "http://example.test/shell", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		proxyURL, err := dialer.Proxy(req)
		if err != nil {
			t.Fatalf("proxy lookup: %v", err)
		}
		if proxyURL != nil {
			t.Fatalf("proxy = %v, want nil", proxyURL)
		}
	}
}

func TestRunWaitsForSocketOutputAfterStdinEOF(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for i := 0; i < 2; i++ {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Errorf("read message %d: %v", i, err)
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
		if err := conn.WriteMessage(websocket.TextMessage, []byte("after stdin eof\n")); err != nil {
			t.Errorf("write output: %v", err)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := Run(t.Context(), Options{
		BaseURL: server.URL,
		Root:    "/data/project",
		Stdin:   strings.NewReader("exit\n"),
		Stdout:  &out,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got != "after stdin eof\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunSendsEOTAfterStdinEOF(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for i := 0; i < 3; i++ {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("read message %d: %v", i, err)
				return
			}
			if i == 2 && !bytes.Equal(msg, []byte{4}) {
				t.Errorf("stdin EOF message = %#v, want EOT", msg)
				return
			}
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("closed\n"))
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := Run(t.Context(), Options{
		BaseURL: server.URL,
		Root:    "/data/project",
		Stdin:   strings.NewReader("echo hi\n"),
		Stdout:  &out,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunReturnsRemoteShellExitCode(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for i := 0; i < 3; i++ {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Errorf("read message %d: %v", i, err)
				return
			}
		}
		if err := conn.WriteJSON(api.ShellFrame{Type: "exit", ExitCode: 7}); err != nil {
			t.Errorf("write exit frame: %v", err)
		}
	}))
	defer server.Close()

	err := Run(t.Context(), Options{
		BaseURL: server.URL,
		Root:    "/data/project",
		Stdin:   strings.NewReader("exit 7\n"),
		Stdout:  &bytes.Buffer{},
	})
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want ExitError", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitErr.ExitCode())
	}
}

func TestForwardInterruptWritesETX(t *testing.T) {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	written := make(chan []byte, 1)
	forwardInterrupts(signals, done, func(data []byte) error {
		written <- append([]byte(nil), data...)
		return nil
	})
	signals <- os.Interrupt
	var got []byte
	select {
	case got = <-written:
	case <-time.After(time.Second):
		t.Fatal("interrupt was not forwarded")
	}
	close(done)
	if !bytes.Equal(got, []byte{3}) {
		t.Fatalf("forwarded = %#v, want ETX", got)
	}
}
