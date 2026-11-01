# Go Webhook Platform

A production-minded webhook delivery service built in Go. It accepts events, persists them with idempotency, fans them out to registered endpoints, and delivers them asynchronously with HMAC signatures and bounded retries.

## Architecture

```text
Producer
  | POST /v1/events + Idempotency-Key
  v
Go HTTP API ---- PostgreSQL transaction
                    | event + delivery rows
                    v
             Concurrent Go workers
                    |
             Signed HTTP POST
                    |
              Customer endpoint
                    |
         retry with exponential backoff
```

PostgreSQL is the durable queue in this first implementation. Workers claim jobs with `FOR UPDATE SKIP LOCKED`, allowing multiple processes to compete safely without a separate broker. This deliberately keeps the initial vertical slice runnable; Kafka/Redis are documented extension points, not claimed as implemented dependencies.

## Implemented

- REST endpoints to register webhook destinations, list destinations, and publish events.
- PostgreSQL persistence for endpoints, events, and per-endpoint delivery state.
- Atomic event + delivery creation in one transaction.
- Required `Idempotency-Key` on event publishing.
- Concurrent workers using row-level locking and `SKIP LOCKED`.
- HMAC-SHA256 request signatures (`X-Webhook-Signature`).
- Bounded HTTP timeouts, retry with exponential backoff and jitter, max attempts.
- Recovery of stale processing jobs after worker interruption.
- JSON structured logs, graceful shutdown, Docker Compose.

## Run locally

Requirements: Go 1.22+ and Docker Compose.

```bash
make infra
# Wait for PostgreSQL health, then:
go mod tidy
make run
```

The API listens on `http://localhost:8080`.

## API examples

Register a destination:

```bash
curl -X POST http://localhost:8080/v1/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/webhooks"}'
```

The response contains an endpoint ID and a signing secret. Store the secret securely; it is returned only at creation.

Publish an event:

```bash
curl -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-123-created' \
  -d '{"type":"order.created","data":{"order_id":"123","amount":499}}'
```

The API returns `202 Accepted`. Each enabled endpoint receives an asynchronous delivery.

## Delivery contract

- A 2xx response is considered successful; all other status codes and transport errors are retried.
- Delivery is **at least once**, not exactly once. A receiver should deduplicate using `X-Webhook-Delivery`.
- Signature format: `sha256=<hex HMAC-SHA256 of the raw JSON payload>`.
- Retry delay is exponential (approximately 2^attempt seconds), jittered, and capped at one hour.
- Once the configured attempt limit is reached, the delivery enters `failed`. A replay/admin API is a planned extension.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| DATABASE_URL | local PostgreSQL URL | Database connection |
| HTTP_ADDR | :8080 | HTTP listen address |
| WORKER_CONCURRENCY | 8 | Concurrent delivery loops |
| REQUEST_TIMEOUT_SECONDS | 10 | Per-attempt HTTP timeout |
| MAX_ATTEMPTS | 8 | Maximum delivery attempts |

## Schema and useful queries

See [migrations/001_init.sql](migrations/001_init.sql). Inspect failed deliveries with:

```sql
SELECT id, event_id, endpoint_id, attempts, last_error
FROM deliveries WHERE status = 'failed'
ORDER BY updated_at DESC;
```

## Engineering roadmap

1. Add endpoint update/disable and secret rotation APIs.
2. Add an outbox publisher and Kafka transport for high-volume deployments.
3. Add Redis-backed per-destination rate limits and circuit breakers.
4. Add dead-letter replay/admin APIs and delivery listing.
5. Add Prometheus metrics and OpenTelemetry traces.
6. Add integration tests with ephemeral PostgreSQL and receiver fault injection.
7. Add load tests and operational dashboards.

## Important security note

This is a portfolio-grade reference implementation, not a hardened public SaaS. Before exposing it to untrusted traffic, add SSRF protections (including DNS/IP revalidation and redirect restrictions), endpoint ownership/authentication, secret encryption/rotation, request quotas, payload limits at the proxy, and stronger tenant isolation.
