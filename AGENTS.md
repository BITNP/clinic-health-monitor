# clinic-health-monitor

## Project

Standalone Go service that checks the clinic backend and admin frontend over
HTTP and sends outage and recovery messages to maintainers through
`message-gateway`. It does not talk to NapCat directly and does not own message
queues, pacing, or retries; those belong to the gateway.

## Rules

- Keep the service single-purpose: monitor the fixed clinic targets (backend
  and admin frontend). Do not turn it into a general monitoring platform.
- Run one `internal.Monitor` per configured target. Each target owns its own
  failure and recovery state; the process is still a single monitor instance.
- Use the standard library. Do not add dependencies without an explicit reason.
- Keep the flat `internal` package split by responsibility across
  `config.go`, `checker.go`, `notifier.go`, `alert.go`, `monitor.go`, and
  `httpapi.go`. Add new files to the same package rather than creating
  subpackages.
- Outbound calls must use `context.Context` and bounded HTTP timeouts.
- Never log `MESSAGE_GATEWAY_ACCOUNT_TOKEN` or other secrets.
- The monitor's own `/healthz` is a liveness endpoint only. It must not report
  the target backend's health.
- Production code lives in the flat `internal` package. The service entrypoint
  is under `cmd/clinic-health-monitor/`. Tests live under `tests/`, mirroring
  the package they exercise, and use external `_test` packages. A test that
  needs private helpers may stay beside its package.
- Update `README.md`, `API.md`, and `.env.example` in the same change whenever
  behavior or configuration changes.
- Run before finishing:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```
