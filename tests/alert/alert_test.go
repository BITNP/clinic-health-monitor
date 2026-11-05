package alert_test

import (
	"clinic-health-monitor/internal"
	"errors"
	"strings"
	"testing"
	"time"
)

var testLocation = time.FixedZone("CST", 8*60*60)

func TestFormatDown(t *testing.T) {
	firstFailure := time.Date(2026, 11, 6, 14, 5, 0, 0, testLocation)
	text := internal.FormatDown(internal.Down{
		Instance:     "clinic-prod",
		TargetName:   "clinic-backend",
		TargetURL:    "http://backend:8080/healthz",
		FirstFailure: firstFailure,
		Failures:     3,
		Result: internal.Result{
			StatusCode: 503,
			Body:       `{"status":"unavailable","redis":"down"}`,
		},
		Location: testLocation,
	})

	for _, want := range []string{
		"[clinic-health-monitor:clinic-prod] clinic-backend DOWN",
		"Target: http://backend:8080/healthz",
		"First failure: 2026-11-06 14:05:00 +08:00",
		"Consecutive failures: 3",
		"Last error: HTTP 503 Service Unavailable",
		`Body: {"status":"unavailable","redis":"down"}`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("FormatDown() = %q, missing %q", text, want)
		}
	}
}

func TestFormatDownWithTransportError(t *testing.T) {
	text := internal.FormatDown(internal.Down{
		Instance:     "clinic-prod",
		TargetName:   "clinic-backend",
		TargetURL:    "http://backend:8080/healthz",
		FirstFailure: time.Now(),
		Failures:     1,
		Result: internal.Result{
			Err: errors.New("dial tcp 10.0.0.1:8080: connect: connection refused"),
		},
		Location: testLocation,
	})

	if !strings.Contains(text, "Last error: Error: dial tcp") {
		t.Errorf("FormatDown() = %q", text)
	}
}

func TestFormatDownTruncatesAndCollapsesDetail(t *testing.T) {
	longBody := strings.Repeat("x", 1000)
	text := internal.FormatDown(internal.Down{
		Instance:     "clinic-prod",
		TargetName:   "clinic-backend",
		TargetURL:    "http://backend:8080/healthz",
		FirstFailure: time.Now(),
		Failures:     3,
		Result: internal.Result{
			StatusCode: 500,
			Body:       "line one\nline two\n" + longBody,
		},
		Location: testLocation,
	})

	if strings.Contains(text, "line one\nline two") {
		t.Error("FormatDown() did not collapse body whitespace")
	}
	if !strings.Contains(text, "line one line two") {
		t.Errorf("FormatDown() = %q", text)
	}
	if !strings.Contains(text, "...") {
		t.Error("FormatDown() did not mark truncated body")
	}
	if strings.Contains(text, longBody) {
		t.Error("FormatDown() contains the untruncated body")
	}
}

func TestFormatRecovery(t *testing.T) {
	downSince := time.Date(2026, 11, 6, 14, 5, 0, 0, testLocation)
	recoveredAt := downSince.Add(2*time.Minute + 30*time.Second)

	text := internal.FormatRecovery(internal.Recovery{
		Instance:    "clinic-prod",
		TargetName:  "clinic-backend",
		TargetURL:   "http://backend:8080/healthz",
		DownSince:   downSince,
		RecoveredAt: recoveredAt,
		Location:    testLocation,
	})

	for _, want := range []string{
		"[clinic-health-monitor:clinic-prod] clinic-backend RECOVERED",
		"Down since: 2026-11-06 14:05:00 +08:00",
		"Recovered at: 2026-11-06 14:07:30 +08:00",
		"Downtime: 2m30s",
		"Target: http://backend:8080/healthz",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("FormatRecovery() = %q, missing %q", text, want)
		}
	}
}
