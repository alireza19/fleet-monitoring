# Writeup

## Time spent + hardest part

The majority of the time was spent getting a sufficient system design based on
the research I was doing with Claude & testing the system. I am new to Golang so
reviewing the code with the new project architecture was the biggest bottleneck.
The most difficult part was examining why the numbers were off. It was
discovered that the calculation for uptime was different giving a different
number as explained below.

## Uptime formula (simulator-driven correction)

The original spec sketch put uptime at `count / (max - min + 1) × 100`. The
Phase 2.5 simulator reported every device's uptime low by exactly one in the
denominator (e.g., 474 of 480 minutes returning 98.5447 instead of 98.75).
The simulator generates heartbeats over an interval where `max - min` equals
its expected denominator, so the corrected formula is
`count / (max - min) × 100`. The fix is a one-line change in
`internal/metrics/stats.go:Uptime`, with `last == first` short-circuiting to
100% to avoid divide-by-zero for the single-minute case. A regression test
captured the misunderstanding before the fix went in.

## Extending the data model

Today the per-device state has two hard-coded shapes: a heartbeat minute set
and an upload count/sum pair. Adding a third metric (say, packet-loss
percentage) means touching the store struct, three call sites, and the
snapshot type.

The clean refactor: introduce a `MetricAggregator` interface

```go
type MetricAggregator interface {
    Record(value float64, t time.Time)
    Snapshot() any
}
```

and replace `deviceMetrics` with `map[string]MetricAggregator`. Each metric
becomes its own implementation (`HeartbeatMinutes`, `UploadAggregator`, etc.)
with its own locking discipline, and handlers dispatch on metric name.

I deliberately did **not** build this now. Two metrics is below the
"three or more" threshold where an interface earns its keep, and the spec
isn't going to grow within this assessment. The refactor path is a few hours
of work if/when the third metric arrives.

## Complexity

- **Heartbeat insert:** O(1) — one map insert under per-device lock.
- **Upload insert:** O(1) — two int64 increments under per-device lock.
- **GET stats:** O(M) where M is the number of distinct active minutes.
  The minute-set iteration computes first/last; the rest is constant work.

The O(M) on GET is a deliberate trade. We could maintain `firstMinute` and
`lastMinute` incrementally on every insert (two int64 compares + writes per
heartbeat) for O(1) GET, but that adds work to the hot path to save µs on
the cold one. If GET ever became hot, that's the targeted optimization —
single-file change in `store.go`.

## AI tool usage

Used Claude (Claude Code) for scaffolding, Go idiom review during pairing,
and to draft test tables from `TEST_CASES.md`. Logic and test design driven
by TDD throughout — every production change had a failing test in front of
it, including the Phase 2.5 simulator regression. `[Ali — adjust to match
what actually happened.]`

## Production caveats

- **In-memory state is lost on restart.** A real fleet service needs durable
  storage. Options: a time-series store (InfluxDB, TimescaleDB) keyed by
  `(device_id, minute_bucket)`, or Postgres with a unique index on the same
  columns and `INSERT ... ON CONFLICT DO NOTHING` for heartbeat dedup.
- **`int64` nanosecond sum overflow.** At `MaxInt64` ≈ 9.2 × 10¹⁸ ns, the
  sum overflows after roughly 290 years of cumulative upload time per
  device. Practically unreachable, but bounded. Mitigation: windowed
  aggregate (last N hours) or HDR histogram with a retention policy that
  resets the sum on rollover.
- **Single-process scaling.** Per-device locking shards writes well within
  one process, but a single process can't survive a real fleet's heartbeat
  rate or provide HA. Production setup: queue-fronted ingestion (Kafka or
  similar) feeding worker pods that write per-device aggregates to Redis
  hashes (HINCRBY for the upload sum, SADD with TTL for minute buckets); a
  read-side service computes `uptime` and `avg_upload_time` on demand from
  the same Redis. Devices and ingest workers scale horizontally.
- **Process-local registry.** The CSV manifest is loaded once at startup; a
  rolling restart is required to add a device. Mitigation: pull the device
  list from a control-plane API or a config store that supports live
  reload.

## Spec deviation: 500 for malformed bodies

The OpenAPI spec enumerates only `204`, `404`, and `500` for the POST
endpoints. HTTP-correct behavior for a malformed body is `400 Bad Request`,
but emitting a 400 would violate the spec. I chose conformance over
correctness because the simulator validates against the spec, and a
spec-conformant 500 keeps it green. Production-ready code should either
push the spec back to include 400 or redirect malformed-body errors to a
separate observability path.

## Security, testing, deployment (brief)

- **Security.** Devices should authenticate with mTLS or per-device API
  keys provisioned at registration. The service itself should sit behind
  an API gateway (rate limits, WAF, request-size caps) — the registry
  should not be the only authorization layer. The body-logging
  middleware is dev-only; never enable `-debug` in production (it would
  log device payloads to stdout).
- **Testing.** Unit + integration coverage is solid for happy paths and
  malformed-input branches. Gaps before production: fuzz-testing the JSON
  decoders (`go-fuzz` or stdlib `testing.F`) and load-testing the
  heartbeat hot path with realistic timing distributions to validate the
  per-device-mutex sharding holds under load. The race detector catches
  data races but not contention regressions; add `go test -bench` or a
  proper load harness.
- **Deployment.** Multi-stage Dockerfile (static binary in `scratch`)
  behind a load balancer, once state is externalized. Health endpoint
  is already in (`/healthz`). Next adds: structured JSON logging in
  place of chi's stdlib logger, and Prometheus metrics for request
  latency histograms plus per-device active-count gauges so on-call can
  spot a silent device.
