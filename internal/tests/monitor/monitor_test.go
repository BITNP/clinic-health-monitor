package monitor_test

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"clinic-health-monitor/internal/checker"
	"clinic-health-monitor/internal/monitor"
	"clinic-health-monitor/internal/notifier"
)

var baseTime = time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)

type fakeChecker struct {
	mu        sync.Mutex
	calls     int
	result    checker.Result
	checkFunc func(call int) checker.Result
}

func (f *fakeChecker) Check(context.Context) checker.Result {
	f.mu.Lock()
	f.calls++
	call := f.calls
	checkFunc := f.checkFunc
	result := f.result
	f.mu.Unlock()

	if checkFunc != nil {
		return checkFunc(call)
	}
	return result
}

func (f *fakeChecker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeNotifier struct {
	mu       sync.Mutex
	calls    int
	texts    []string
	result   notifier.SendResult
	err      error
	sendFunc func(call int) (notifier.SendResult, error)
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{
		result: notifier.SendResult{Attempted: 1, Succeeded: 1},
	}
}

func (f *fakeNotifier) Send(_ context.Context, text string) (notifier.SendResult, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.texts = append(f.texts, text)
	sendFunc := f.sendFunc
	result := f.result
	err := f.err
	f.mu.Unlock()

	if sendFunc != nil {
		return sendFunc(call)
	}
	return result, err
}

func (f *fakeNotifier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeNotifier) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

func testConfig() monitor.Config {
	return monitor.Config{
		TargetName:        "clinic-backend",
		TargetURL:         "http://backend:8080/healthz",
		InstanceName:      "test",
		Interval:          time.Hour,
		FailureThreshold:  3,
		RecoveryThreshold: 1,
		NotifyRecovery:    true,
		Location:          time.UTC,
	}
}

func newTestMonitor(cfg monitor.Config, healthChecker monitor.Checker, gateway monitor.Notifier) *monitor.Monitor {
	return monitor.New(cfg, healthChecker, gateway, log.New(io.Discard, "", 0))
}

func healthyAt(at time.Time) checker.Result {
	return checker.Result{Healthy: true, StatusCode: 200, CheckedAt: at}
}

func failedAt(at time.Time, status int) checker.Result {
	return checker.Result{StatusCode: status, Body: `{"status":"unavailable"}`, CheckedAt: at}
}

func sequenceChecker(results ...checker.Result) *fakeChecker {
	return &fakeChecker{
		checkFunc: func(call int) checker.Result {
			if call > len(results) {
				return results[len(results)-1]
			}
			return results[call-1]
		},
	}
}

func TestHealthyTargetDoesNotNotify(t *testing.T) {
	healthChecker := &fakeChecker{result: healthyAt(baseTime)}
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 3; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 0 {
		t.Errorf("notification calls = %d, want 0", calls)
	}
}

func TestBelowFailureThresholdDoesNotNotify(t *testing.T) {
	healthChecker := &fakeChecker{result: failedAt(baseTime, 503)}
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 2; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 0 {
		t.Errorf("notification calls = %d, want 0", calls)
	}
}

func TestFailureThresholdSendsOneAlert(t *testing.T) {
	healthChecker := &fakeChecker{
		checkFunc: func(call int) checker.Result {
			return failedAt(baseTime.Add(time.Duration(call-1)*time.Minute), 503)
		},
	}
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 5; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 1 {
		t.Fatalf("notification calls = %d, want 1", calls)
	}
	text := gateway.messages()[0]
	for _, want := range []string{
		"clinic-backend DOWN",
		"First failure: 2026-11-06 14:00:00 +00:00",
		"Consecutive failures: 3",
		"HTTP 503 Service Unavailable",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("alert = %q, missing %q", text, want)
		}
	}
}

func TestFailedAlertIsRetried(t *testing.T) {
	healthChecker := &fakeChecker{result: failedAt(baseTime, 503)}
	gateway := newFakeNotifier()
	gateway.sendFunc = func(call int) (notifier.SendResult, error) {
		if call == 1 {
			return notifier.SendResult{Attempted: 1, Failed: 1}, errors.New("gateway down")
		}
		return notifier.SendResult{Attempted: 1, Succeeded: 1}, nil
	}
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 5; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 2 {
		t.Errorf("notification calls = %d, want 2", calls)
	}
}

func TestPartialAlertSuccessClearsPending(t *testing.T) {
	healthChecker := &fakeChecker{result: failedAt(baseTime, 503)}
	gateway := newFakeNotifier()
	gateway.sendFunc = func(int) (notifier.SendResult, error) {
		return notifier.SendResult{Attempted: 2, Succeeded: 1, Failed: 1}, errors.New("one maintainer failed")
	}
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 5; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 1 {
		t.Errorf("notification calls = %d, want 1", calls)
	}
}

func TestRecoveryAfterThreshold(t *testing.T) {
	healthChecker := sequenceChecker(
		failedAt(baseTime, 503),
		failedAt(baseTime.Add(time.Minute), 503),
		failedAt(baseTime.Add(2*time.Minute), 503),
		healthyAt(baseTime.Add(3*time.Minute)),
	)
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 4; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 2 {
		t.Fatalf("notification calls = %d, want 2", calls)
	}
	text := gateway.messages()[1]
	for _, want := range []string{
		"clinic-backend RECOVERED",
		"Down since: 2026-11-06 14:00:00 +00:00",
		"Downtime: 3m0s",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("recovery = %q, missing %q", text, want)
		}
	}
}

func TestRecoveryRequiresConsecutiveSuccesses(t *testing.T) {
	cfg := testConfig()
	cfg.RecoveryThreshold = 2
	healthChecker := sequenceChecker(
		failedAt(baseTime, 503),
		failedAt(baseTime.Add(time.Minute), 503),
		failedAt(baseTime.Add(2*time.Minute), 503),
		healthyAt(baseTime.Add(3*time.Minute)),
		healthyAt(baseTime.Add(4*time.Minute)),
	)
	gateway := newFakeNotifier()
	m := newTestMonitor(cfg, healthChecker, gateway)

	for i := 0; i < 4; i++ {
		m.CheckOnce(context.Background())
	}
	if calls := gateway.callCount(); calls != 1 {
		t.Fatalf("notification calls after one success = %d, want 1", calls)
	}

	m.CheckOnce(context.Background())
	if calls := gateway.callCount(); calls != 2 {
		t.Fatalf("notification calls after two successes = %d, want 2", calls)
	}
}

func TestRecoveryNotSentWhenAlertNeverDelivered(t *testing.T) {
	healthChecker := sequenceChecker(
		failedAt(baseTime, 503),
		failedAt(baseTime.Add(time.Minute), 503),
		failedAt(baseTime.Add(2*time.Minute), 503),
		healthyAt(baseTime.Add(3*time.Minute)),
	)
	gateway := newFakeNotifier()
	gateway.result = notifier.SendResult{Attempted: 1, Failed: 1}
	gateway.err = errors.New("gateway down")
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 4; i++ {
		m.CheckOnce(context.Background())
	}

	messages := gateway.messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want only the down alert", len(messages))
	}
	if strings.Contains(messages[0], "RECOVERED") {
		t.Errorf("unexpected recovery message: %q", messages[0])
	}
}

func TestAlertRepeat(t *testing.T) {
	cfg := testConfig()
	cfg.AlertRepeatInterval = 30 * time.Second
	healthChecker := &fakeChecker{
		checkFunc: func(call int) checker.Result {
			return failedAt(baseTime.Add(time.Duration(call)*time.Minute), 503)
		},
	}
	gateway := newFakeNotifier()
	m := newTestMonitor(cfg, healthChecker, gateway)

	for i := 0; i < 4; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 2 {
		t.Errorf("notification calls = %d, want 2", calls)
	}
}

func TestNoAlertRepeatByDefault(t *testing.T) {
	healthChecker := &fakeChecker{
		checkFunc: func(call int) checker.Result {
			return failedAt(baseTime.Add(time.Duration(call)*time.Hour), 503)
		},
	}
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 5; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 1 {
		t.Errorf("notification calls = %d, want 1", calls)
	}
}

func TestNewOutageAfterRecoverySendsNewAlert(t *testing.T) {
	healthChecker := sequenceChecker(
		failedAt(baseTime, 503),
		failedAt(baseTime.Add(time.Minute), 503),
		failedAt(baseTime.Add(2*time.Minute), 503),
		healthyAt(baseTime.Add(3*time.Minute)),
		failedAt(baseTime.Add(4*time.Minute), 503),
		failedAt(baseTime.Add(5*time.Minute), 503),
		failedAt(baseTime.Add(6*time.Minute), 503),
	)
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	for i := 0; i < 7; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 3 {
		t.Fatalf("notification calls = %d, want 3", calls)
	}
	if !strings.Contains(gateway.messages()[0], "DOWN") {
		t.Errorf("first message = %q", gateway.messages()[0])
	}
	if !strings.Contains(gateway.messages()[1], "RECOVERED") {
		t.Errorf("second message = %q", gateway.messages()[1])
	}
	if !strings.Contains(gateway.messages()[2], "DOWN") {
		t.Errorf("third message = %q", gateway.messages()[2])
	}
}

func TestFirstFailureTimeSurvivesShortRecovery(t *testing.T) {
	cfg := testConfig()
	cfg.RecoveryThreshold = 2
	healthChecker := sequenceChecker(
		failedAt(baseTime, 503),
		failedAt(baseTime.Add(time.Minute), 503),
		failedAt(baseTime.Add(2*time.Minute), 503),
		healthyAt(baseTime.Add(3*time.Minute)),
		failedAt(baseTime.Add(4*time.Minute), 503),
		healthyAt(baseTime.Add(5*time.Minute)),
		healthyAt(baseTime.Add(6*time.Minute)),
	)
	gateway := newFakeNotifier()
	m := newTestMonitor(cfg, healthChecker, gateway)

	for i := 0; i < 7; i++ {
		m.CheckOnce(context.Background())
	}

	if calls := gateway.callCount(); calls != 2 {
		t.Fatalf("notification calls = %d, want 2", calls)
	}
	text := gateway.messages()[1]
	if !strings.Contains(text, "Down since: 2026-11-06 14:00:00 +00:00") {
		t.Errorf("recovery = %q, want original down time", text)
	}
	if !strings.Contains(text, "Downtime: 6m0s") {
		t.Errorf("recovery = %q, want six-minute downtime", text)
	}
}

func TestCanceledContextSkipsCheck(t *testing.T) {
	healthChecker := &fakeChecker{result: healthyAt(baseTime)}
	gateway := newFakeNotifier()
	m := newTestMonitor(testConfig(), healthChecker, gateway)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.CheckOnce(ctx)

	if calls := healthChecker.callCount(); calls != 0 {
		t.Errorf("checker calls = %d, want 0", calls)
	}
	if calls := gateway.callCount(); calls != 0 {
		t.Errorf("notification calls = %d, want 0", calls)
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	cfg := testConfig()
	cfg.Interval = time.Millisecond
	healthChecker := &fakeChecker{result: healthyAt(baseTime)}
	gateway := newFakeNotifier()
	m := newTestMonitor(cfg, healthChecker, gateway)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for healthChecker.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
	if calls := healthChecker.callCount(); calls == 0 {
		t.Error("Run() did not perform a check")
	}
}
