// Package alert formats outage and recovery messages for maintainers.
package internal

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxDetailRunes = 300

// Down describes one outage notification.
type Down struct {
	Instance     string
	TargetName   string
	TargetURL    string
	FirstFailure time.Time
	Failures     int
	Result       Result
	Location     *time.Location
}

// Recovery describes one recovery notification.
type Recovery struct {
	Instance    string
	TargetName  string
	TargetURL   string
	DownSince   time.Time
	RecoveredAt time.Time
	Location    *time.Location
}

// FormatDown builds the alert text sent when the target stays unhealthy.
func FormatDown(info Down) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[clinic-health-monitor:%s] %s DOWN\n", info.Instance, info.TargetName)
	fmt.Fprintf(&b, "Target: %s\n", info.TargetURL)
	fmt.Fprintf(&b, "First failure: %s\n", formatTime(info.FirstFailure, info.Location))
	fmt.Fprintf(&b, "Consecutive failures: %d\n", info.Failures)
	fmt.Fprintf(&b, "Last error: %s", formatResult(info.Result))
	return b.String()
}

// FormatRecovery builds the alert text sent after the target recovers.
func FormatRecovery(info Recovery) string {
	downtime := info.RecoveredAt.Sub(info.DownSince)
	if downtime < 0 {
		downtime = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[clinic-health-monitor:%s] %s RECOVERED\n", info.Instance, info.TargetName)
	fmt.Fprintf(&b, "Down since: %s\n", formatTime(info.DownSince, info.Location))
	fmt.Fprintf(&b, "Recovered at: %s\n", formatTime(info.RecoveredAt, info.Location))
	fmt.Fprintf(&b, "Downtime: %s\n", downtime.Round(time.Second))
	fmt.Fprintf(&b, "Target: %s", info.TargetURL)
	return b.String()
}

func formatResult(result Result) string {
	var b strings.Builder
	if result.Err != nil {
		fmt.Fprintf(&b, "Error: %s", truncate(result.Err.Error()))
		return b.String()
	}

	fmt.Fprintf(&b, "HTTP %d", result.StatusCode)
	if text := http.StatusText(result.StatusCode); text != "" {
		fmt.Fprintf(&b, " %s", text)
	}
	if body := truncate(result.Body); body != "" {
		fmt.Fprintf(&b, "\nBody: %s", body)
	}
	return b.String()
}

func formatTime(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return "unknown"
	}
	if location == nil {
		location = time.Local
	}
	return value.In(location).Format("2006-01-02 15:04:05 -07:00")
}

func truncate(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}

	runes := []rune(value)
	if len(runes) <= maxDetailRunes {
		return value
	}
	return string(runes[:maxDetailRunes]) + "..."
}
