package checker_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clinic-health-monitor/internal"
)

func TestCheckHealthy(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(` {"status":"ok","redis":"up"} `))
	}))
	defer server.Close()

	result := internal.NewHTTPChecker(server.URL, time.Second).Check(context.Background())
	if !result.Healthy {
		t.Fatalf("Healthy = false, error = %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", result.StatusCode)
	}
	if result.Body != `{"status":"ok","redis":"up"}` {
		t.Errorf("Body = %q", result.Body)
	}
	if result.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero")
	}
	if userAgent != "clinic-health-monitor" {
		t.Errorf("User-Agent = %q", userAgent)
	}
}

func TestCheckNoContentIsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	result := internal.NewHTTPChecker(server.URL, time.Second).Check(context.Background())
	if !result.Healthy {
		t.Fatalf("Healthy = false, error = %v", result.Err)
	}
	if result.StatusCode != http.StatusNoContent {
		t.Errorf("StatusCode = %d", result.StatusCode)
	}
}

func TestCheckNon2xxIsUnhealthy(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "service unavailable", status: http.StatusServiceUnavailable, body: `{"status":"unavailable","redis":"down"}`},
		{name: "internal error", status: http.StatusInternalServerError, body: "boom"},
		{name: "not found", status: http.StatusNotFound, body: "missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			result := internal.NewHTTPChecker(server.URL, time.Second).Check(context.Background())
			if result.Healthy {
				t.Fatal("Healthy = true, want false")
			}
			if result.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", result.StatusCode, tt.status)
			}
			if result.Body != tt.body {
				t.Errorf("Body = %q, want %q", result.Body, tt.body)
			}
			if result.Err != nil {
				t.Errorf("Err = %v, want nil", result.Err)
			}
		})
	}
}

func TestCheckDoesNotFollowRedirects(t *testing.T) {
	var startHits, okHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			startHits++
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/ok":
			okHits++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := internal.NewHTTPChecker(server.URL+"/start", time.Second).Check(context.Background())
	if result.Healthy {
		t.Fatal("Healthy = true, want false")
	}
	if result.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusFound)
	}
	if startHits != 1 || okHits != 0 {
		t.Errorf("hits start=%d ok=%d, want start=1 ok=0", startHits, okHits)
	}
}

func TestCheckTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := internal.NewHTTPChecker(server.URL, 20*time.Millisecond).Check(context.Background())
	if result.Healthy {
		t.Fatal("Healthy = true, want false")
	}
	if result.Err == nil {
		t.Fatal("Err = nil, want timeout error")
	}
}

func TestCheckConnectionRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	result := internal.NewHTTPChecker("http://"+address, time.Second).Check(context.Background())
	if result.Healthy {
		t.Fatal("Healthy = true, want false")
	}
	if result.Err == nil {
		t.Fatal("Err = nil, want connection error")
	}
}

func TestCheckCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := internal.NewHTTPChecker(server.URL, time.Second).Check(ctx)
	if result.Healthy {
		t.Fatal("Healthy = true, want false")
	}
	if result.Err == nil {
		t.Fatal("Err = nil, want context error")
	}
}

func TestCheckLimitsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("a", 5000))
	}))
	defer server.Close()

	result := internal.NewHTTPChecker(server.URL, time.Second).Check(context.Background())
	if len(result.Body) > 4096 {
		t.Errorf("Body length = %d, want at most 4096", len(result.Body))
	}
	if len(result.Body) == 0 {
		t.Error("Body is empty")
	}
}
