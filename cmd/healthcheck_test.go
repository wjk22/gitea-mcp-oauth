package cmd

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRunHealthcheck(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		port       func(server *httptest.Server) int
		wantOK     bool
		wantStdout string
	}{
		{
			name: "server responds 200 OK",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			port:       serverPort,
			wantOK:     true,
			wantStdout: "healthy",
		},
		{
			name: "server responds with an error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			port:   serverPort,
			wantOK: false,
		},
		{
			name:   "nothing listening on the port",
			port:   func(*httptest.Server) int { return 1 },
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.handler != nil {
				server = httptest.NewServer(tt.handler)
				defer server.Close()
			}

			var stdout, stderr bytes.Buffer
			ok := runHealthcheck(http.DefaultClient, tt.port(server), &stdout, &stderr)

			if ok != tt.wantOK {
				t.Errorf("runHealthcheck() = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantOK && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty on success", stderr.String())
			}
			if !tt.wantOK && stderr.Len() == 0 {
				t.Error("stderr is empty, want a failure message")
			}
		})
	}
}

func serverPort(server *httptest.Server) int {
	_, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		panic(err)
	}
	return port
}
