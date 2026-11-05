# clinic-health-monitor

## Project

Standalone Go service that checks `clinic-backend` over HTTP and sends outage
and recovery messages to maintainers through `message-gateway`. It does not
talk to NapCat directly and does not own message queues, pacing, or retries;
those belong to the gateway.

## Rules

- Keep the service single-purpose: one health target, one monitor instance.
- Use the standard library. Do not add dependencies without an explicit reason.
- Keep `internal/config`, `internal/checker`, `internal/notifier`,
  `internal/alert`, `internal/monitor`, and `internal/httpapi` separated by
  responsibility.
- Outbound calls must use `context.Context` and bounded HTTP timeouts.
- Never log `MESSAGE_GATEWAY_API_KEY` or other secrets.
- The monitor's own `/healthz` is a liveness endpoint only. It must not report
  the target backend's health.
- All Go code lives under `internal/`. Tests live under `internal/tests/`,
  mirroring the package they exercise, and use external `_test` packages. A
  test that needs private helpers may stay beside its package.
- Update `README.md`, `API.md`, and `.env.example` in the same change whenever
  behavior or configuration changes.
- Run before finishing:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```
