package main

import (
	"clinic-health-monitor/internal"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg, err := internal.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	if cfg.Timeout >= cfg.Interval {
		log.Printf(
			"warning: MONITOR_TIMEOUT (%s) is not shorter than MONITOR_INTERVAL (%s); checks may be skipped",
			cfg.Timeout,
			cfg.Interval,
		)
	}

	healthChecker := internal.New(cfg.TargetURL, cfg.Timeout)
	gateway := internal.New(internal.Config{
		BaseURL: cfg.GatewayURL,
		APIKey:  cfg.GatewayAPIKey,
		Timeout: cfg.GatewayTimeout,
	}, cfg.MaintainerQQIDs)

	watcher := internal.New(internal.Config{
		TargetName:          cfg.TargetName,
		TargetURL:           cfg.TargetURL,
		InstanceName:        cfg.InstanceName,
		Interval:            cfg.Interval,
		FailureThreshold:    cfg.FailureThreshold,
		RecoveryThreshold:   cfg.RecoveryThreshold,
		AlertRepeatInterval: cfg.AlertRepeatInterval,
		NotifyRecovery:      cfg.NotifyRecovery,
		Location:            cfg.Location,
	}, healthChecker, gateway, log.Default())

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		log.Fatalf("listen on %s: %v", cfg.HTTPAddr, err)
	}

	server := &http.Server{
		Handler:           internal.New(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	runCtx, cancel := context.WithCancel(context.Background())
	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		watcher.Run(runCtx)
	}()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	log.Printf(
		"clinic-health-monitor listening on %s (target %s, interval %s, failure threshold %d, recovery threshold %d, maintainers %d)",
		cfg.HTTPAddr,
		cfg.TargetURL,
		cfg.Interval,
		cfg.FailureThreshold,
		cfg.RecoveryThreshold,
		len(cfg.MaintainerQQIDs),
	)

	exitCode := 0
	select {
	case sig := <-signals:
		log.Printf("received %s, shutting down", sig)
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server error: %v", err)
			exitCode = 1
		}
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
		_ = server.Close()
		exitCode = 1
	}
	shutdownCancel()

	background.Wait()
	log.Println("clinic-health-monitor stopped")

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
