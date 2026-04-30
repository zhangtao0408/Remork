package shellclient

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
