package notifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clinic-health-monitor/internal"
)

type capturedRequest struct {
	Method        string
	Path          string
	Authorization string
	ContentType   string
	Body          sendBody
}

type sendBody struct {
	Platform string `json:"platform"`
	Target   struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"target"`
	Text string `json:"text"`
}

type fakeGateway struct {
	mu       sync.Mutex
	requests []capturedRequest
	handle   func(w http.ResponseWriter, body sendBody)
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body sendBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.requests = append(f.requests, capturedRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		ContentType:   r.Header.Get("Content-Type"),
		Body:          body,
	})
	f.mu.Unlock()

	if f.handle == nil {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"qq-1","status":"queued"}`))
		return
	}
	f.handle(w, body)
}

func (f *fakeGateway) captured() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedRequest(nil), f.requests...)
}

func newClient(t *testing.T, server *httptest.Server, targets ...string) *internal.Client {
	t.Helper()
	return internal.NewNotifier(internal.NotifierConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Timeout: time.Second,
	}, targets)
}

func TestSendDeliversToEveryMaintainer(t *testing.T) {
	gateway := &fakeGateway{}
	server := httptest.NewServer(gateway)
	defer server.Close()

	client := newClient(t, server, "10001", "10002")
	result, err := client.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result != (internal.SendResult{Attempted: 2, Succeeded: 2}) {
		t.Errorf("Send() result = %+v", result)
	}

	requests := gateway.captured()
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	for i, request := range requests {
		if request.Method != http.MethodPost {
			t.Errorf("Method = %q", request.Method)
		}
		if request.Path != "/api/v1/messages" {
			t.Errorf("Path = %q", request.Path)
		}
		if request.Authorization != "Bearer test-key" {
			t.Errorf("Authorization = %q", request.Authorization)
		}
		if !strings.HasPrefix(request.ContentType, "application/json") {
			t.Errorf("Content-Type = %q", request.ContentType)
		}
		if request.Body.Platform != "qq" {
			t.Errorf("Platform = %q", request.Body.Platform)
		}
		if request.Body.Target.Kind != "user" {
			t.Errorf("Target.Kind = %q", request.Body.Target.Kind)
		}
		if request.Body.Target.ID != []string{"10001", "10002"}[i] {
			t.Errorf("Target.ID = %q", request.Body.Target.ID)
		}
		if request.Body.Text != "hello" {
			t.Errorf("Text = %q", request.Body.Text)
		}
	}
}

func TestSendTrimsTrailingSlashFromBaseURL(t *testing.T) {
	gateway := &fakeGateway{}
	server := httptest.NewServer(gateway)
	defer server.Close()

	client := internal.NewNotifier(internal.NotifierConfig{
		BaseURL: server.URL + "/",
		APIKey:  "test-key",
		Timeout: time.Second,
	}, []string{"10001"})

	if _, err := client.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	requests := gateway.captured()
	if len(requests) != 1 || requests[0].Path != "/api/v1/messages" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestSendReturnsPermanentGatewayErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "bad request", status: http.StatusBadRequest},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "too large", status: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &fakeGateway{handle: func(w http.ResponseWriter, _ sendBody) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}}
			server := httptest.NewServer(gateway)
			defer server.Close()

			result, err := newClient(t, server, "10001").Send(context.Background(), "hello")
			if err == nil {
				t.Fatal("Send() error = nil, want error")
			}
			if result != (internal.SendResult{Attempted: 1, Failed: 1}) {
				t.Errorf("Send() result = %+v", result)
			}
			if requests := gateway.captured(); len(requests) != 1 {
				t.Errorf("request count = %d, want 1", len(requests))
			}
		})
	}
}

func TestSendRetriesRateLimit(t *testing.T) {
	var attempts int
	gateway := &fakeGateway{handle: func(w http.ResponseWriter, _ sendBody) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"message queue is full"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}}
	server := httptest.NewServer(gateway)
	defer server.Close()

	result, err := newClient(t, server, "10001").Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Succeeded != 1 {
		t.Errorf("Send() result = %+v", result)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestSendRetriesServerErrors(t *testing.T) {
	gateway := &fakeGateway{handle: func(w http.ResponseWriter, _ sendBody) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}}
	server := httptest.NewServer(gateway)
	defer server.Close()

	result, err := newClient(t, server, "10001").Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("Send() error = nil, want error")
	}
	if result != (internal.SendResult{Attempted: 1, Failed: 1}) {
		t.Errorf("Send() result = %+v", result)
	}
	if requests := gateway.captured(); len(requests) != 3 {
		t.Errorf("request count = %d, want 3", len(requests))
	}
}

func TestSendContinuesAfterPartialFailure(t *testing.T) {
	gateway := &fakeGateway{handle: func(w http.ResponseWriter, body sendBody) {
		if body.Target.ID == "10001" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}}
	server := httptest.NewServer(gateway)
	defer server.Close()

	result, err := newClient(t, server, "10001", "10002").Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("Send() error = nil, want partial error")
	}
	if result != (internal.SendResult{Attempted: 2, Succeeded: 1, Failed: 1}) {
		t.Errorf("Send() result = %+v", result)
	}
	if requests := gateway.captured(); len(requests) != 2 {
		t.Errorf("request count = %d, want 2", len(requests))
	}
}

func TestSendStopsWhenContextIsCanceled(t *testing.T) {
	gateway := &fakeGateway{}
	server := httptest.NewServer(gateway)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := newClient(t, server, "10001", "10002").Send(ctx, "hello")
	if err == nil {
		t.Fatal("Send() error = nil, want context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
	if result != (internal.SendResult{Attempted: 2, Failed: 2}) {
		t.Errorf("Send() result = %+v", result)
	}
	if requests := gateway.captured(); len(requests) != 0 {
		t.Errorf("request count = %d, want 0", len(requests))
	}
}
