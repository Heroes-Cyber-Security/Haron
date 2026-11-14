## Forwarder

Forwards Ethereum JSON-RPC traffic to an orchestrator while exposing both HTTP and WebSocket entrypoints under `/eth/{id}`. Requests are routed to `http://orchestrator:8080/anvil/{id}` and WebSocket connections support subscription flows such as `eth_subscribe`.

The forwarder is the player-facing RPC gate: the webui nginx routes both `/anvil/` and `/eth/` here, and every JSON-RPC method is checked against a denylist (HTTP and WebSocket, single and batch) before it reaches the orchestrator.

### Configuration

Environment variables:

- `FORWARDER_LISTEN_ADDR` (default `:8080`) — address the service binds to.
- `ORCHESTRATOR_URL` (default `http://orchestrator:8080`) — base URL for the orchestrator instance.

### Development

```bash
cd src
go test ./...
```

The service exposes a simple `GET /healthz` endpoint that returns `200 OK` for readiness checks.