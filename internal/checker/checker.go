// Package checker performs one HTTP health check against the configured target.
package checker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBodyBytes = 4 << 10

// Result describes one health-check attempt.
type Result struct {
	Healthy    bool
	StatusCode int
	Body       string
	Err        error
	CheckedAt  time.Time
}

// Checker performs HTTP health checks.
type Checker struct {
	target string
	client *http.Client
}

// New creates a checker for target. Non-2xx responses are unhealthy, and
// redirects are not followed.
func New(target string, timeout time.Duration) *Checker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The target is an internal service; do not route it through a proxy.
	transport.Proxy = nil

	return &Checker{
		target: target,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Check performs one health-check attempt. The returned result is always
// populated with CheckedAt so callers can report when the attempt happened.
func (c *Checker) Check(ctx context.Context) Result {
	checkedAt := time.Now()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.target, nil)
	if err != nil {
		return Result{Err: fmt.Errorf("build request: %w", err), CheckedAt: checkedAt}
	}
	request.Header.Set("User-Agent", "clinic-health-monitor")
	request.Header.Set("Accept", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		return Result{Err: err, CheckedAt: checkedAt}
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	result := Result{
		StatusCode: response.StatusCode,
		Body:       strings.TrimSpace(string(body)),
		CheckedAt:  checkedAt,
	}
	if readErr != nil {
		result.Err = fmt.Errorf("read response body: %w", readErr)
		return result
	}

	result.Healthy = response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	return result
}
