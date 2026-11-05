# clinic-health-monitor

Standalone service that watches `clinic-backend`'s `/healthz` endpoint and
notifies maintainers through `message-gateway` when the backend stays unhealthy.

The implementation is being built in stages. See `AGENTS.md` for project rules.
