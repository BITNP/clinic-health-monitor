package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clinic-health-monitor/internal"
)

type sendBody struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

type sentMessage struct {
	TargetID string
	Text     string
}

func TestAlertAndRecoveryReachEveryMaintainer(t *testing.T) {
	var healthy atomic.Bool

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","redis":"up"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unavailable","redis":"down"}`))
	}))
	defer backend.Close()

	var mu sync.Mutex
	var messages []sentMessage
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/qq/send_private_msg_async" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var body sendBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		messages = append(messages, sentMessage{TargetID: body.UserID, Text: body.Message})
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"qq-1","status":"queued"}`))
	}))
	defer gateway.Close()

	gatewayClient := internal.NewNotifier(internal.NotifierConfig{
		BaseURL:      gateway.URL + "/api/v1/accounts/qq",
		AccountToken: "test-token",
		Timeout:      time.Second,
	}, []string{"10001", "10002"})

	watcher := internal.NewMonitor(internal.MonitorConfig{
		TargetName:        "clinic-backend",
		TargetURL:         backend.URL,
		InstanceName:      "test",
		Interval:          time.Hour,
		FailureThreshold:  1,
		RecoveryThreshold: 1,
		NotifyRecovery:    true,
		Location:          time.UTC,
	}, internal.NewHTTPChecker(backend.URL, time.Second), gatewayClient, log.New(io.Discard, "", 0))

	healthy.Store(false)
	watcher.CheckOnce(context.Background())

	mu.Lock()
	alertMessages := append([]sentMessage(nil), messages...)
	mu.Unlock()
	if len(alertMessages) != 2 {
		t.Fatalf("alert messages = %d, want 2", len(alertMessages))
	}
	for _, message := range alertMessages {
		if !strings.Contains(message.Text, "clinic-backend DOWN") {
			t.Errorf("alert = %q", message.Text)
		}
		if !strings.Contains(message.Text, "HTTP 503 Service Unavailable") {
			t.Errorf("alert = %q, missing status detail", message.Text)
		}
	}
	if alertMessages[0].TargetID != "10001" || alertMessages[1].TargetID != "10002" {
		t.Errorf("alert targets = %q, %q", alertMessages[0].TargetID, alertMessages[1].TargetID)
	}

	healthy.Store(true)
	watcher.CheckOnce(context.Background())

	mu.Lock()
	allMessages := append([]sentMessage(nil), messages...)
	mu.Unlock()
	if len(allMessages) != 4 {
		t.Fatalf("total messages = %d, want 4", len(allMessages))
	}
	for _, message := range allMessages[2:] {
		if !strings.Contains(message.Text, "clinic-backend RECOVERED") {
			t.Errorf("recovery = %q", message.Text)
		}
	}
}

func TestTargetsAlertIndependently(t *testing.T) {
	var backendHealthy atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if backendHealthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "backend down", http.StatusServiceUnavailable)
	}))
	defer backend.Close()

	var frontendHealthy atomic.Bool
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if frontendHealthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "frontend down", http.StatusServiceUnavailable)
	}))
	defer frontend.Close()

	var mu sync.Mutex
	var messages []sentMessage
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/qq/send_private_msg_async" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var body sendBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		messages = append(messages, sentMessage{TargetID: body.UserID, Text: body.Message})
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"qq-1","status":"queued"}`))
	}))
	defer gateway.Close()

	gatewayClient := internal.NewNotifier(internal.NotifierConfig{
		BaseURL:      gateway.URL + "/api/v1/accounts/qq",
		AccountToken: "test-token",
		Timeout:      time.Second,
	}, []string{"10001", "10002"})

	newMonitor := func(name, targetURL string) *internal.Monitor {
		return internal.NewMonitor(internal.MonitorConfig{
			TargetName:        name,
			TargetURL:         targetURL,
			InstanceName:      "test",
			Interval:          time.Hour,
			FailureThreshold:  1,
			RecoveryThreshold: 1,
			NotifyRecovery:    true,
			Location:          time.UTC,
		}, internal.NewHTTPChecker(targetURL, time.Second), gatewayClient, log.New(io.Discard, "", 0))
	}

	backendMonitor := newMonitor("clinic-backend", backend.URL)
	frontendMonitor := newMonitor("clinic-admin-frontend", frontend.URL)

	backendHealthy.Store(false)
	frontendHealthy.Store(true)
	backendMonitor.CheckOnce(context.Background())
	frontendMonitor.CheckOnce(context.Background())

	mu.Lock()
	firstRound := append([]sentMessage(nil), messages...)
	mu.Unlock()
	if len(firstRound) != 2 {
		t.Fatalf("alert messages = %d, want 2", len(firstRound))
	}
	for _, message := range firstRound {
		if !strings.Contains(message.Text, "clinic-backend DOWN") {
			t.Errorf("alert = %q, want backend alert", message.Text)
		}
		if strings.Contains(message.Text, "clinic-admin-frontend DOWN") {
			t.Errorf("alert = %q, frontend must stay quiet", message.Text)
		}
	}

	backendHealthy.Store(true)
	frontendHealthy.Store(false)
	backendMonitor.CheckOnce(context.Background())
	frontendMonitor.CheckOnce(context.Background())

	mu.Lock()
	allMessages := append([]sentMessage(nil), messages...)
	mu.Unlock()
	if len(allMessages) != 6 {
		t.Fatalf("total messages = %d, want 6", len(allMessages))
	}
	for _, message := range allMessages[2:4] {
		if !strings.Contains(message.Text, "clinic-backend RECOVERED") {
			t.Errorf("recovery = %q, want backend recovery", message.Text)
		}
	}
	for _, message := range allMessages[4:] {
		if !strings.Contains(message.Text, "clinic-admin-frontend DOWN") {
			t.Errorf("alert = %q, want frontend alert", message.Text)
		}
	}
}
