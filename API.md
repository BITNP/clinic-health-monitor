# API Reference

HTTP endpoints exposed by `clinic-health-monitor`. The server listens on
`MONITOR_HTTP_ADDR` (default `:8082`). There is no authentication; keep the
endpoint on an internal network.

## `GET /healthz`

**Description:** Liveness check for the monitor process. Public. It does not
check any configured target, the message gateway, or NapCat. A `200` only means
this process is running.

**Request:** No body, no parameters.

**Response `200 OK`**

```json
{ "status": "ok" }
```

**curl**

```bash
curl http://127.0.0.1:8082/healthz
```

Other methods return `405 Method Not Allowed`. Unknown paths return `404 Not
Found`.

## Outbound calls

The monitor does not accept alert requests over HTTP. It calls the configured
`message-gateway` endpoint itself; see `message-gateway/API.md` for that
account-scoped `send_private_msg_async` endpoint and its delivery semantics.
`MESSAGE_GATEWAY_URL` specifies the full account base URL, and the monitor
appends `/send_private_msg_async`.
