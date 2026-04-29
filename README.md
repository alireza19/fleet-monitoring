# Fleet Monitoring

In-memory HTTP service that aggregates per-device heartbeats and upload
statistics for a fixed fleet loaded from a CSV manifest at startup. Built
for the SafelyYou Fleet Management coding assessment; the OpenAPI contract
is the source of truth for status codes and request/response shapes.

## Quickstart

```sh
make run        # starts the server on :6733 with devices.csv from the project root
make test       # unit + integration tests
make test-race  # same suite under the race detector
make build      # produces ./bin/fleet-server
make lint       # golangci-lint (errcheck, govet, staticcheck, revive, gofmt, goimports)
make tidy       # go mod tidy
```

The server reads its device manifest at startup and serves the API on a
configurable port. State is in-memory only; restarts lose all aggregates.

## Flags

| Flag      | Default        | Purpose |
| --------- | -------------- | ------- |
| `-port`   | `6733`         | HTTP listen port |
| `-csv`    | `devices.csv`  | Path to the device manifest |
| `-debug`  | `false`        | Logs request bodies for `POST /api/v1/devices/*` paths. Useful when iterating against the simulator. |

```sh
./bin/fleet-server -port 6733 -csv devices.csv -debug
```

## Endpoints

API base path: `/api/v1`. See [`openapi.json`](./openapi.json) for the
full contract — status codes, request/response schemas, and error envelope.

| Method | Path                                  | Purpose |
| ------ | ------------------------------------- | ------- |
| `POST` | `/api/v1/devices/{device_id}/heartbeat` | Record a per-minute heartbeat. Same-minute heartbeats dedupe. |
| `POST` | `/api/v1/devices/{device_id}/stats`     | Record one upload duration sample (nanoseconds). |
| `GET`  | `/api/v1/devices/{device_id}/stats`     | Return `{uptime, avg_upload_time}` for the device. |
| `GET`  | `/healthz`                            | Liveness probe. Returns `{"status":"ok"}`. Outside the API group; no device lookup. |

Successful POSTs return `204 No Content` with an empty body. Unknown
device IDs return `404` with `{"msg":"device not found"}`. Malformed
bodies return `500` with `{"msg":"..."}` (per the OpenAPI spec, which
enumerates only 404/500 for these endpoints — see WRITEUP.md for the
deliberate spec deviation here).

## Running the simulator

The simulator binary is a separate artifact provided with the assessment.
Start the server first, optionally with `-debug` so request bodies are
logged for diagnosis, then point the simulator at the same port:

```sh
./bin/fleet-server -port 6733 -csv devices.csv -debug &
curl -s http://localhost:6733/healthz   # sanity check
./device-simulator                       # whatever the simulator binary is named
```

## Layout

```
cmd/server/          # entry point, flag parsing, graceful shutdown
internal/device/     # registry: load + lookup of valid device IDs
internal/metrics/    # store (heartbeat sets, upload aggregates) + stats (pure projection functions)
internal/api/        # handlers, response helpers, router, middleware
devices.csv          # device manifest read at startup
openapi.json         # API contract (source of truth)
```

Top-level type sketch: `Registry` (immutable after `Load`) + `Store` (per-device
`*deviceMetrics` behind an `RWMutex`, top-level map immutable after `NewStore`).
`Handlers` carry both via constructor injection. The router applies chi's
`RequestID`, `Logger`, and `Recoverer` middleware; the optional body-logging
middleware is added when `-debug` is set.
