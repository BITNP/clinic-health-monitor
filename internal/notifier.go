package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxAttempts      = 3
	retryBackoff     = 500 * time.Millisecond
	maxResponseBytes = 4 << 10
)

// NotifierConfig contains the gateway connection settings.
type NotifierConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// SendResult summarizes one notification fan-out.
type SendResult struct {
	Attempted int
	Succeeded int
	Failed    int
}

// Client sends private QQ messages to a fixed set of maintainers.
type Client struct {
	endpoint string
	apiKey   string
	targets  []string
	client   *http.Client
}

type sendRequest struct {
	Platform string     `json:"platform"`
	Target   sendTarget `json:"target"`
	Text     string     `json:"text"`
}

type sendTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// NewNotifier creates a gateway client. The caller is expected to have
// validated the base URL and target ids.
func NewNotifier(cfg NotifierConfig, targets []string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil

	return &Client{
		endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/messages",
		apiKey:   cfg.APIKey,
		targets:  append([]string(nil), targets...),
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Send delivers text to every maintainer. A partial failure still returns the
// successful count so callers can decide whether an alert reached anyone.
func (c *Client) Send(ctx context.Context, text string) (SendResult, error) {
	result := SendResult{Attempted: len(c.targets)}
	var errs []error

	for i, target := range c.targets {
		if err := ctx.Err(); err != nil {
			result.Failed += len(c.targets) - i
			errs = append(errs, err)
			break
		}

		if err := c.sendTo(ctx, target, text); err != nil {
			result.Failed++
			errs = append(errs, fmt.Errorf("send to %s: %w", target, err))
			continue
		}
		result.Succeeded++
	}

	if len(errs) > 0 {
		return result, errors.Join(errs...)
	}
	return result, nil
}

func (c *Client) sendTo(ctx context.Context, target, text string) error {
	payload, err := json.Marshal(sendRequest{
		Platform: "qq",
		Target:   sendTarget{Kind: "user", ID: target},
		Text:     text,
	})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		delay, retryable, err := c.attempt(ctx, payload)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			return lastErr
		}
		if err := sleepContext(ctx, delay); err != nil {
			return fmt.Errorf("%w: %v", lastErr, err)
		}
	}
	return lastErr
}

func (c *Client) attempt(ctx context.Context, payload []byte) (time.Duration, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, false, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		return retryBackoff, true, fmt.Errorf("post message: %w", err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	snippet := strings.TrimSpace(string(body))

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return 0, false, nil
	}

	message := fmt.Sprintf("gateway returned %s", response.Status)
	if snippet != "" {
		message += ": " + snippet
	}

	if response.StatusCode == http.StatusTooManyRequests {
		return retryDelay(response, retryBackoff), true, errors.New(message)
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return retryBackoff, true, errors.New(message)
	}
	return 0, false, errors.New(message)
}

func retryDelay(response *http.Response, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(response.Header.Get("Retry-After"))
	if raw == "" {
		return fallback
	}

	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds < 0 {
			return fallback
		}
		return time.Duration(seconds) * time.Second
	}

	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			return 0
		}
		return delay
	}
	return fallback
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
