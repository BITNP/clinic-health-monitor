package config_test

import (
	"clinic-health-monitor/internal"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"MONITOR_TARGET_URL":      "http://127.0.0.1:8080/healthz",
		"MAINTAINER_QQ_IDS":       "10001,10002",
		"MESSAGE_GATEWAY_API_KEY": "test-key",
	}
}

func loadEnv(env map[string]string) (internal.Config, error) {
	return internal.Load(func(key string) string {
		return env[key]
	})
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := loadEnv(validEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TargetURL != "http://127.0.0.1:8080/healthz" {
		t.Errorf("TargetURL = %q", cfg.TargetURL)
	}
	if cfg.TargetName != "clinic-backend" {
		t.Errorf("TargetName = %q", cfg.TargetName)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %s", cfg.Interval)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %s", cfg.Timeout)
	}
	if cfg.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d", cfg.FailureThreshold)
	}
	if cfg.RecoveryThreshold != 1 {
		t.Errorf("RecoveryThreshold = %d", cfg.RecoveryThreshold)
	}
	if cfg.AlertRepeatInterval != 0 {
		t.Errorf("AlertRepeatInterval = %s", cfg.AlertRepeatInterval)
	}
	if !cfg.NotifyRecovery {
		t.Error("NotifyRecovery = false")
	}
	if cfg.HTTPAddr != ":8082" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.InstanceName != "clinic-health-monitor" {
		t.Errorf("InstanceName = %q", cfg.InstanceName)
	}
	if cfg.GatewayURL != "http://message-gateway:8081" {
		t.Errorf("GatewayURL = %q", cfg.GatewayURL)
	}
	if cfg.GatewayTimeout != 5*time.Second {
		t.Errorf("GatewayTimeout = %s", cfg.GatewayTimeout)
	}
	if len(cfg.MaintainerQQIDs) != 2 || cfg.MaintainerQQIDs[0] != "10001" || cfg.MaintainerQQIDs[1] != "10002" {
		t.Errorf("MaintainerQQIDs = %v", cfg.MaintainerQQIDs)
	}
}

func TestLoadCustomValues(t *testing.T) {
	env := validEnv()
	env["MONITOR_TARGET_NAME"] = "production-backend"
	env["MONITOR_INTERVAL"] = "1m"
	env["MONITOR_TIMEOUT"] = "3s"
	env["MONITOR_FAILURE_THRESHOLD"] = "5"
	env["MONITOR_RECOVERY_THRESHOLD"] = "2"
	env["MONITOR_ALERT_REPEAT_INTERVAL"] = "1h"
	env["MONITOR_NOTIFY_RECOVERY"] = "false"
	env["MONITOR_HTTP_ADDR"] = ":9090"
	env["MONITOR_INSTANCE_NAME"] = "clinic-prod"
	env["MESSAGE_GATEWAY_URL"] = "https://gateway.example.com"
	env["MESSAGE_GATEWAY_TIMEOUT"] = "2s"
	env["TZ"] = "UTC"

	cfg, err := loadEnv(env)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TargetName != "production-backend" {
		t.Errorf("TargetName = %q", cfg.TargetName)
	}
	if cfg.Interval != time.Minute {
		t.Errorf("Interval = %s", cfg.Interval)
	}
	if cfg.Timeout != 3*time.Second {
		t.Errorf("Timeout = %s", cfg.Timeout)
	}
	if cfg.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d", cfg.FailureThreshold)
	}
	if cfg.RecoveryThreshold != 2 {
		t.Errorf("RecoveryThreshold = %d", cfg.RecoveryThreshold)
	}
	if cfg.AlertRepeatInterval != time.Hour {
		t.Errorf("AlertRepeatInterval = %s", cfg.AlertRepeatInterval)
	}
	if cfg.NotifyRecovery {
		t.Error("NotifyRecovery = true")
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.InstanceName != "clinic-prod" {
		t.Errorf("InstanceName = %q", cfg.InstanceName)
	}
	if cfg.GatewayURL != "https://gateway.example.com" {
		t.Errorf("GatewayURL = %q", cfg.GatewayURL)
	}
	if cfg.GatewayTimeout != 2*time.Second {
		t.Errorf("GatewayTimeout = %s", cfg.GatewayTimeout)
	}
	if cfg.Location != time.UTC {
		t.Errorf("Location = %v", cfg.Location)
	}
}

func TestMaintainerIDsAreTrimmedAndDeduplicated(t *testing.T) {
	env := validEnv()
	env["MAINTAINER_QQ_IDS"] = " 10001, 10002,10001 ,10003 "

	cfg, err := loadEnv(env)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []string{"10001", "10002", "10003"}
	if len(cfg.MaintainerQQIDs) != len(want) {
		t.Fatalf("MaintainerQQIDs = %v, want %v", cfg.MaintainerQQIDs, want)
	}
	for i := range want {
		if cfg.MaintainerQQIDs[i] != want[i] {
			t.Errorf("MaintainerQQIDs[%d] = %q, want %q", i, cfg.MaintainerQQIDs[i], want[i])
		}
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name:    "missing target URL",
			mutate:  func(env map[string]string) { delete(env, "MONITOR_TARGET_URL") },
			wantErr: "MONITOR_TARGET_URL",
		},
		{
			name:    "target URL without scheme",
			mutate:  func(env map[string]string) { env["MONITOR_TARGET_URL"] = "backend:8080/healthz" },
			wantErr: "http or https",
		},
		{
			name:    "target URL without host",
			mutate:  func(env map[string]string) { env["MONITOR_TARGET_URL"] = "http://" },
			wantErr: "host",
		},
		{
			name:    "invalid interval",
			mutate:  func(env map[string]string) { env["MONITOR_INTERVAL"] = "soon" },
			wantErr: "MONITOR_INTERVAL",
		},
		{
			name:    "zero interval",
			mutate:  func(env map[string]string) { env["MONITOR_INTERVAL"] = "0s" },
			wantErr: "must be positive",
		},
		{
			name:    "negative repeat interval",
			mutate:  func(env map[string]string) { env["MONITOR_ALERT_REPEAT_INTERVAL"] = "-1s" },
			wantErr: "must not be negative",
		},
		{
			name:    "invalid recovery bool",
			mutate:  func(env map[string]string) { env["MONITOR_NOTIFY_RECOVERY"] = "sometimes" },
			wantErr: "MONITOR_NOTIFY_RECOVERY",
		},
		{
			name:    "zero failure threshold",
			mutate:  func(env map[string]string) { env["MONITOR_FAILURE_THRESHOLD"] = "0" },
			wantErr: "must be positive",
		},
		{
			name:    "missing maintainers",
			mutate:  func(env map[string]string) { delete(env, "MAINTAINER_QQ_IDS") },
			wantErr: "MAINTAINER_QQ_IDS",
		},
		{
			name:    "empty maintainer id",
			mutate:  func(env map[string]string) { env["MAINTAINER_QQ_IDS"] = "10001,,10002" },
			wantErr: "empty id",
		},
		{
			name:    "non-numeric maintainer id",
			mutate:  func(env map[string]string) { env["MAINTAINER_QQ_IDS"] = "10001,abc" },
			wantErr: "non-numeric",
		},
		{
			name:    "missing gateway key",
			mutate:  func(env map[string]string) { delete(env, "MESSAGE_GATEWAY_API_KEY") },
			wantErr: "MESSAGE_GATEWAY_API_KEY",
		},
		{
			name:    "invalid gateway URL",
			mutate:  func(env map[string]string) { env["MESSAGE_GATEWAY_URL"] = "message-gateway:8081" },
			wantErr: "MESSAGE_GATEWAY_URL",
		},
		{
			name:    "invalid timezone",
			mutate:  func(env map[string]string) { env["TZ"] = "Mars/Olympus" },
			wantErr: "TZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			tt.mutate(env)

			_, err := loadEnv(env)
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}
