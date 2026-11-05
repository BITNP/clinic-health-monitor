// Package monitor polls the target and manages outage/recovery notifications.
package monitor

import (
	"context"
	"log"
	"time"

	"clinic-health-monitor/internal/alert"
	"clinic-health-monitor/internal/checker"
	"clinic-health-monitor/internal/notifier"
)

// Checker performs one health check.
type Checker interface {
	Check(context.Context) checker.Result
}

// Notifier delivers one formatted message to the maintainers.
type Notifier interface {
	Send(context.Context, string) (notifier.SendResult, error)
}

// Config contains the monitor's scheduling and alerting settings.
type Config struct {
	TargetName   string
	TargetURL    string
	InstanceName string
	Interval     time.Duration

	FailureThreshold    int
	RecoveryThreshold   int
	AlertRepeatInterval time.Duration
	NotifyRecovery      bool
	Location            *time.Location
}

type state int

const (
	stateUnknown state = iota
	stateHealthy
	stateDown
)

// Monitor tracks consecutive results and sends one alert per outage.
type Monitor struct {
	cfg      Config
	checker  Checker
	notifier Notifier
	logger   *log.Logger

	state state

	failures       int
	successes      int
	firstFailureAt time.Time
	lastAlertAt    time.Time

	alertPending bool
	alertSent    bool
	alertText    string
}

// New creates a monitor. A nil logger uses the standard logger.
func New(cfg Config, healthChecker Checker, gateway Notifier, logger *log.Logger) *Monitor {
	if logger == nil {
		logger = log.Default()
	}
	return &Monitor{
		cfg:      cfg,
		checker:  healthChecker,
		notifier: gateway,
		logger:   logger,
	}
}

// Run performs an immediate check, then polls until the context is canceled.
func (m *Monitor) Run(ctx context.Context) {
	m.CheckOnce(ctx)

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CheckOnce(ctx)
		}
	}
}

// CheckOnce performs one health check and applies the state machine.
func (m *Monitor) CheckOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	result := m.checker.Check(ctx)
	if ctx.Err() != nil {
		return
	}

	checkedAt := result.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}

	if result.Healthy {
		m.handleHealthy(ctx, result, checkedAt)
		return
	}
	m.handleFailure(ctx, result, checkedAt)
}

func (m *Monitor) handleHealthy(ctx context.Context, result checker.Result, checkedAt time.Time) {
	m.failures = 0
	m.successes++

	if m.state != stateDown {
		m.state = stateHealthy
		return
	}

	if m.successes < m.cfg.RecoveryThreshold {
		return
	}

	downSince := m.firstFailureAt
	downtime := checkedAt.Sub(downSince)
	if downSince.IsZero() || downtime < 0 {
		downtime = 0
	}

	m.state = stateHealthy
	m.logger.Printf("%s recovered after %s", m.cfg.TargetName, downtime.Round(time.Second))

	if m.cfg.NotifyRecovery && m.alertSent {
		text := alert.FormatRecovery(alert.Recovery{
			Instance:    m.cfg.InstanceName,
			TargetName:  m.cfg.TargetName,
			TargetURL:   m.cfg.TargetURL,
			DownSince:   downSince,
			RecoveredAt: checkedAt,
			Location:    m.cfg.Location,
		})
		m.sendRecovery(ctx, text)
	}

	m.successes = 0
	m.firstFailureAt = time.Time{}
	m.lastAlertAt = time.Time{}
	m.alertPending = false
	m.alertSent = false
	m.alertText = ""
}

func (m *Monitor) handleFailure(ctx context.Context, result checker.Result, checkedAt time.Time) {
	m.successes = 0
	if m.state != stateDown && m.failures == 0 {
		m.firstFailureAt = checkedAt
	}
	m.failures++

	if m.state != stateDown {
		if m.failures < m.cfg.FailureThreshold {
			return
		}

		m.state = stateDown
		m.logger.Printf("%s unhealthy after %d consecutive failures", m.cfg.TargetName, m.failures)
		m.alertText = m.formatDown(result)
		m.alertPending = true
		m.sendAlert(ctx, checkedAt)
		return
	}

	if m.alertPending {
		m.alertText = m.formatDown(result)
		m.sendAlert(ctx, checkedAt)
		return
	}

	if m.cfg.AlertRepeatInterval > 0 && checkedAt.Sub(m.lastAlertAt) >= m.cfg.AlertRepeatInterval {
		m.alertText = m.formatDown(result)
		m.alertPending = true
		m.sendAlert(ctx, checkedAt)
	}
}

func (m *Monitor) formatDown(result checker.Result) string {
	return alert.FormatDown(alert.Down{
		Instance:     m.cfg.InstanceName,
		TargetName:   m.cfg.TargetName,
		TargetURL:    m.cfg.TargetURL,
		FirstFailure: m.firstFailureAt,
		Failures:     m.failures,
		Result:       result,
		Location:     m.cfg.Location,
	})
}

func (m *Monitor) sendAlert(ctx context.Context, checkedAt time.Time) {
	if m.alertText == "" {
		return
	}

	result, err := m.notifier.Send(ctx, m.alertText)
	if result.Succeeded > 0 {
		if err != nil {
			m.logger.Printf("alert partially delivered: %d/%d maintainer(s) succeeded: %v", result.Succeeded, result.Attempted, err)
		} else {
			m.logger.Printf("alert delivered to %d maintainer(s)", result.Succeeded)
		}
		m.alertPending = false
		m.alertSent = true
		m.lastAlertAt = checkedAt
		return
	}

	if err != nil {
		m.logger.Printf("alert delivery failed: %v", err)
	} else {
		m.logger.Printf("alert was not delivered to any maintainer")
	}
}

func (m *Monitor) sendRecovery(ctx context.Context, text string) {
	result, err := m.notifier.Send(ctx, text)
	if result.Succeeded > 0 {
		if err != nil {
			m.logger.Printf("recovery message partially delivered: %d/%d maintainer(s) succeeded: %v", result.Succeeded, result.Attempted, err)
		} else {
			m.logger.Printf("recovery message delivered to %d maintainer(s)", result.Succeeded)
		}
		return
	}

	if err != nil {
		m.logger.Printf("recovery message delivery failed: %v", err)
	} else {
		m.logger.Printf("recovery message was not delivered to any maintainer")
	}
}
