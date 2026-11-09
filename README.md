# clinic-health-monitor

Standalone service that watches the clinic backend's `/healthz` endpoint and
the admin frontend's `/` endpoint, and notifies maintainers through
`message-gateway` when either target stays unhealthy.

It owns only the polling and alerting state machines. QQ/NapCat transport,
queues, pacing, and delivery retries belong to `message-gateway`.

## Features

- Backend and admin frontend targets with independent outage state.
- Configurable health-check interval, timeout, and failure/recovery thresholds.
- One outage alert per maintainer instead of one alert per failed poll.
- Optional repeat alerts for long outages.
- Optional recovery notifications.
- Private QQ messages through the authenticated `message-gateway` HTTP API.
- Public liveness endpoint for the monitor process itself.
- Standard-library Go implementation with no external dependencies.
- Docker image and standalone Compose stack.

## Health contract

The monitor calls each configured target:

- `MONITOR_TARGET_URL` should be the backend's existing `GET /healthz` endpoint.
- `MONITOR_FRONTEND_URL` is optional and should be the admin frontend's `GET /`
  endpoint.

The contract is the same for every target:

- `2xx` means healthy.
- Any non-`2xx`, redirect, timeout, DNS failure, or connection error means
  unhealthy.
- The response body is not parsed in v1; the HTTP status code is the contract.
- A response-body snippet is included in alert messages for diagnostics.

The frontend `/` check proves Caddy is running and serving the built
`index.html`. It does not execute JavaScript or verify the public Traefik/TLS
path.

The current backend `/healthz` only checks Redis. A database or object-storage
failure may still return `200`. If deeper coverage is needed, add a backend
`/readyz` endpoint and point `MONITOR_TARGET_URL` at it.

## Alert behavior

Each target uses three states: `Unknown`, `Healthy`, and `Down`. Targets have
independent failure counters, alerts, and recovery tracking.

- Failures are counted consecutively.
- After `MONITOR_FAILURE_THRESHOLD` failures, the target enters `Down` and one
  alert is sent to every configured maintainer.
- While the target stays down, no duplicate alert is sent unless
  `MONITOR_ALERT_REPEAT_INTERVAL` is greater than zero.
- If an alert could not be delivered, later checks retry it.
- After `MONITOR_RECOVERY_THRESHOLD` consecutive successes, the target returns
  to `Healthy`.
- A recovery message is sent when `MONITOR_NOTIFY_RECOVERY=true` and an outage
  alert was actually delivered to at least one maintainer.

With the defaults, an outage is reported after roughly 60–90 seconds. If the
whole clinic pod is down, maintainers may receive one backend alert and one
frontend alert.

Example outage message:

```text
[clinic-health-monitor:clinic-prod] clinic-backend DOWN
Target: http://backend:8080/healthz
First failure: 2026-11-06 14:05:00 +08:00
Consecutive failures: 3
Last error: HTTP 503 Service Unavailable
Body: {"status":"unavailable","redis":"down"}
```

Example recovery message:

```text
[clinic-health-monitor:clinic-prod] clinic-backend RECOVERED
Down since: 2026-11-06 14:05:00 +08:00
Recovered at: 2026-11-06 14:08:12 +08:00
Downtime: 3m12s
Target: http://backend:8080/healthz
```

## Configuration

All settings are environment variables. Configuration is read once at startup;
restart the service after changing it.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `MONITOR_TARGET_URL` | yes | — | Backend health endpoint. |
| `MONITOR_TARGET_NAME` | no | `clinic-backend` | Backend target name used in messages. |
| `MONITOR_FRONTEND_URL` | no | — | Optional admin frontend endpoint. Empty disables frontend monitoring. |
| `MONITOR_FRONTEND_NAME` | no | `clinic-admin-frontend` | Frontend target name used in messages. |
| `MONITOR_INTERVAL` | no | `30s` | Delay between checks. |
| `MONITOR_TIMEOUT` | no | `5s` | Per-check HTTP timeout. |
| `MONITOR_FAILURE_THRESHOLD` | no | `3` | Consecutive failures before an outage alert. |
| `MONITOR_RECOVERY_THRESHOLD` | no | `1` | Consecutive successes before recovery. |
| `MONITOR_ALERT_REPEAT_INTERVAL` | no | `0s` | Repeat alert interval while down; `0s` disables repeats. |
| `MONITOR_NOTIFY_RECOVERY` | no | `true` | Send a recovery message. |
| `MONITOR_HTTP_ADDR` | no | `:8082` | Liveness HTTP listen address. |
| `MONITOR_INSTANCE_NAME` | no | `clinic-health-monitor` | Instance name used in messages. |
| `MAINTAINER_QQ_IDS` | yes | — | Comma-separated numeric QQ user ids. Duplicates are removed. |
| `MESSAGE_GATEWAY_URL` | no | `http://message-gateway:8081` | Gateway base URL. |
| `MESSAGE_GATEWAY_API_KEY` | yes | — | Gateway bearer token. |
| `MESSAGE_GATEWAY_TIMEOUT` | no | `5s` | Per-request gateway timeout. |
| `TZ` | no | process local | Timezone used in message timestamps. |

Compose-only variables are documented in `.env.example`.

Invalid configuration stops startup with a clear error. The gateway API key is
never printed.

## Local development

Run the monitor as a host binary:

```bash
MONITOR_TARGET_URL=http://127.0.0.1:8080/healthz \
MESSAGE_GATEWAY_URL=http://127.0.0.1:8081 \
MESSAGE_GATEWAY_API_KEY=replace-with-message-gateway-key \
MAINTAINER_QQ_IDS=10001 \
go run ./cmd/clinic-health-monitor
```

Add `MONITOR_FRONTEND_URL=http://127.0.0.1:5173/` to also check a locally
running admin frontend.

The existing `clinic-backend/scripts/dev.sh` starts the backend, Redis, and
`message-gateway`; it does not start this monitor.

### Test without a real QQ account

Start `message-gateway` and the fake NapCat stack from `message-gateway`:

```bash
cd ../message-gateway
docker compose up -d
docker compose -f docker-compose.fake-napcat.yml up -d
```

Then run the monitor with:

```bash
MONITOR_TARGET_URL=http://127.0.0.1:9/healthz \
MESSAGE_GATEWAY_URL=http://127.0.0.1:8081 \
MESSAGE_GATEWAY_API_KEY=replace-with-message-gateway-key \
MAINTAINER_QQ_IDS=10001 \
MONITOR_INTERVAL=1s \
MONITOR_FAILURE_THRESHOLD=1 \
MONITOR_TIMEOUT=1s \
go run ./cmd/clinic-health-monitor
```

`10001` is a friend in the fake NapCat. Wait for the alert, then inspect:

```bash
curl http://127.0.0.1:3001/_messages
```

To test recovery, keep the monitor running and make the same target start
returning `200` again. A small local stub whose response can be switched, or
stopping and restarting the backend while the monitor stays up, works. Do not
restart the monitor: its state is in-memory, so a restart while the target is
healthy starts a new baseline instead of sending a recovery message.

## Docker Compose

The standalone stack is for local or alternative deployments. It expects the
`clinic-backend` and `message-gateway` external networks to exist.

```bash
cp .env.example .env
chmod 600 .env
# Set MAINTAINER_QQ_IDS and MESSAGE_GATEWAY_API_KEY.
docker compose up -d --build
docker compose logs -f clinic-health-monitor
```

The monitor liveness endpoint is published on `127.0.0.1:8082` by default.

The recommended production deployment adds the monitor to
`clinic-backend/docker-compose.prod.yml`, so it shares the backend's `default`
network and the external `message-gateway` network. See the
`clinic-backend` deployment documentation.

## Liveness

`GET /healthz` on `MONITOR_HTTP_ADDR` reports only whether the monitor process
is running. It does not report any target's health; otherwise Docker would
restart the monitor whenever a target is down. See [API.md](API.md).

## Notification delivery

Each alert is sent as a private QQ message to every id in
`MAINTAINER_QQ_IDS`:

```json
{
  "platform": "qq",
  "target": { "kind": "user", "id": "10001" },
  "text": "..."
}
```

The gateway's `202 Accepted` response means the message was queued, not
delivered. The monitor retries network errors, `429`, and `5xx` responses with
bounded backoff. `400`, `401`, and `413` are treated as configuration errors.

## Troubleshooting

- **No messages arrive**: check `docker compose logs message-gateway` in
  `message-gateway`, confirm the API key matches, and confirm the maintainers
  are friends of the QQ bot or share a group with it.
- **Monitor logs `401 Unauthorized`**: `MESSAGE_GATEWAY_API_KEY` does not match
  the gateway.
- **Monitor logs `429 Too Many Requests`**: the gateway queue is full; the
  monitor retries with backoff.
- **Frequent false alarms**: raise `MONITOR_FAILURE_THRESHOLD` or
  `MONITOR_INTERVAL`, and keep `MONITOR_TIMEOUT` shorter than
  `MONITOR_INTERVAL`.
- **A target is healthy but the monitor reports failure**: verify the monitor
  can reach the configured `MONITOR_TARGET_URL` or `MONITOR_FRONTEND_URL` from
  its container network.

## Limitations

- The backend `/healthz` only checks Redis, not Postgres or S3.
- The frontend `/` check only verifies Caddy serves `index.html`; it does not
  execute JavaScript or check the public Traefik/TLS path.
- A full clinic pod outage can produce one alert per target.
- If `message-gateway` or NapCat is down, alerts cannot be delivered. The
  monitor logs the failure; there is no secondary channel.
- `202` does not guarantee QQ delivery. The gateway queue is in-memory.
- The monitor is single-instance. Multiple replicas would duplicate alerts.
- State is in-memory. Restarting the monitor during an outage re-alerts after
  the threshold.
- If the whole Docker host dies, the monitor dies with it. Use an external
  uptime service if host-level failure detection is required.
- No metrics, dashboard, or paging integration in v1.

## Layout

```text
internal/config.go                   environment loading and validation
internal/checker.go                  one HTTP health check
internal/notifier.go                 message-gateway HTTP client
internal/alert.go                    outage and recovery message formatting
internal/monitor.go                  polling loop and alert state machine
internal/httpapi.go                  liveness endpoint
cmd/clinic-health-monitor/           service entry point
tests/                               external tests
Dockerfile                  multi-stage image
docker-compose.yml          standalone stack
.github/workflows/          CI and GHCR publishing
```

## Development

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

## Images and releases

The Dockerfile builds `ghcr.io/bitnp/clinic-health-monitor`. Pushing a `v*` tag
runs `.github/workflows/docker-publish.yml`, which publishes version, short-SHA,
and `latest` tags. Pin `HEALTH_MONITOR_IMAGE_TAG` in deployments.
