// Package config loads and validates the health monitor's environment
// configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInterval          = 30 * time.Second
	defaultTimeout           = 5 * time.Second
	defaultFailureThreshold  = 3
	defaultRecoveryThreshold = 1
	defaultHTTPAddr          = ":8082"
	defaultInstanceName      = "clinic-health-monitor"
	defaultTargetName        = "clinic-backend"
	defaultGatewayURL        = "http://message-gateway:8081"
	defaultGatewayTimeout    = 5 * time.Second
)

// Config is the validated runtime configuration for the health monitor.
type Config struct {
	TargetURL  string
	TargetName string

	Interval            time.Duration
	Timeout             time.Duration
	FailureThreshold    int
	RecoveryThreshold   int
	AlertRepeatInterval time.Duration
	NotifyRecovery      bool

	HTTPAddr     string
	InstanceName string
	Location     *time.Location

	MaintainerQQIDs []string
	GatewayURL      string
	GatewayAPIKey   string
	GatewayTimeout  time.Duration
}

// Load reads and validates the health monitor environment.
func Load(getenv func(string) string) (Config, error) {
	targetURL, err := validateHTTPURL("MONITOR_TARGET_URL", getenv("MONITOR_TARGET_URL"))
	if err != nil {
		return Config{}, err
	}

	targetName := strings.TrimSpace(getenv("MONITOR_TARGET_NAME"))
	if targetName == "" {
		targetName = defaultTargetName
	}

	interval, err := parsePositiveDuration("MONITOR_INTERVAL", getenv("MONITOR_INTERVAL"), defaultInterval)
	if err != nil {
		return Config{}, err
	}

	timeout, err := parsePositiveDuration("MONITOR_TIMEOUT", getenv("MONITOR_TIMEOUT"), defaultTimeout)
	if err != nil {
		return Config{}, err
	}

	failureThreshold, err := parsePositiveInt("MONITOR_FAILURE_THRESHOLD", getenv("MONITOR_FAILURE_THRESHOLD"), defaultFailureThreshold)
	if err != nil {
		return Config{}, err
	}

	recoveryThreshold, err := parsePositiveInt("MONITOR_RECOVERY_THRESHOLD", getenv("MONITOR_RECOVERY_THRESHOLD"), defaultRecoveryThreshold)
	if err != nil {
		return Config{}, err
	}

	alertRepeatInterval, err := parseNonNegativeDuration("MONITOR_ALERT_REPEAT_INTERVAL", getenv("MONITOR_ALERT_REPEAT_INTERVAL"))
	if err != nil {
		return Config{}, err
	}

	notifyRecovery, err := parseBool("MONITOR_NOTIFY_RECOVERY", getenv("MONITOR_NOTIFY_RECOVERY"), true)
	if err != nil {
		return Config{}, err
	}

	httpAddr := strings.TrimSpace(getenv("MONITOR_HTTP_ADDR"))
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}

	instanceName := strings.TrimSpace(getenv("MONITOR_INSTANCE_NAME"))
	if instanceName == "" {
		instanceName = defaultInstanceName
	}

	maintainerIDs, err := parseMaintainerIDs(getenv("MAINTAINER_QQ_IDS"))
	if err != nil {
		return Config{}, err
	}

	gatewayURL := strings.TrimSpace(getenv("MESSAGE_GATEWAY_URL"))
	if gatewayURL == "" {
		gatewayURL = defaultGatewayURL
	}
	gatewayURL, err = validateHTTPURL("MESSAGE_GATEWAY_URL", gatewayURL)
	if err != nil {
		return Config{}, err
	}

	gatewayAPIKey := strings.TrimSpace(getenv("MESSAGE_GATEWAY_API_KEY"))
	if gatewayAPIKey == "" {
		return Config{}, errors.New("MESSAGE_GATEWAY_API_KEY is required")
	}

	gatewayTimeout, err := parsePositiveDuration("MESSAGE_GATEWAY_TIMEOUT", getenv("MESSAGE_GATEWAY_TIMEOUT"), defaultGatewayTimeout)
	if err != nil {
		return Config{}, err
	}

	location := time.Local
	if tz := strings.TrimSpace(getenv("TZ")); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return Config{}, fmt.Errorf("invalid TZ: %w", err)
		}
		location = loc
	}

	return Config{
		TargetURL:  targetURL,
		TargetName: targetName,

		Interval:            interval,
		Timeout:             timeout,
		FailureThreshold:    failureThreshold,
		RecoveryThreshold:   recoveryThreshold,
		AlertRepeatInterval: alertRepeatInterval,
		NotifyRecovery:      notifyRecovery,

		HTTPAddr:     httpAddr,
		InstanceName: instanceName,
		Location:     location,

		MaintainerQQIDs: maintainerIDs,
		GatewayURL:      gatewayURL,
		GatewayAPIKey:   gatewayAPIKey,
		GatewayTimeout:  gatewayTimeout,
	}, nil
}

func validateHTTPURL(name, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s is required", name)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use http or https", name)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s must include a host", name)
	}
	return raw, nil
}

func parsePositiveInt(name, raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("invalid %s: must be positive", name)
	}
	return value, nil
}

func parsePositiveDuration(name, raw string, fallback time.Duration) (time.Duration, error) {
	value, err := parseDuration(name, raw, fallback)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("invalid %s: must be positive", name)
	}
	return value, nil
}

func parseNonNegativeDuration(name, raw string) (time.Duration, error) {
	value, err := parseDuration(name, raw, 0)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid %s: must not be negative", name)
	}
	return value, nil
}

func parseDuration(name, raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return value, nil
}

func parseBool(name, raw string, fallback bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", name, err)
	}
	return value, nil
}

func parseMaintainerIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("MAINTAINER_QQ_IDS is required")
	}

	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			return nil, errors.New("MAINTAINER_QQ_IDS contains an empty id")
		}
		for _, r := range id {
			if r < '0' || r > '9' {
				return nil, fmt.Errorf("MAINTAINER_QQ_IDS contains a non-numeric id %q", id)
			}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("MAINTAINER_QQ_IDS is required")
	}
	return ids, nil
}
