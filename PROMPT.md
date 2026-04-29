# SafelyYou Coding Challenge — TDD Implementation Prompt

You're helping me build a Go HTTP service for a SafelyYou take-home. I'm experienced but new to Go — explain idioms in inline comments as they appear, but keep teaching commentary tight.

We're using **strict TDD**: red → green → refactor, one test (or one table-driven group) at a time. No production code without a failing test first.

## Repo contents

- `openapi.json` — API contract. Status codes, request/response shapes, base path `/api/v1`. **Source of truth.**
- `devices.csv` — valid device IDs, single column, header row.
- `PROMPT.md` — this file.
- `TEST_CASES.md` — enumerated test cases per package. Consult this every time you're about to write a new test (see "TDD discipline" below).

## What we're building

In-memory HTTP service:
1. Loads `devices.csv` on startup into a registry of valid device IDs.
2. Listens on a port (default `6733`, override `-port`).
3. Three endpoints under `/api/v1`:
   - `POST /devices/{device_id}/heartbeat` → 204 / 404 / 500
   - `POST /devices/{device_id}/stats` → 204 / 404 / 500
   - `GET /devices/{device_id}/stats` → 200 / 404 / 500

A separate simulator (not built here) exercises the POSTs, then GETs each device and checks our outputs.

## Locked-in design

Don't re-litigate these — implement them.

**Stack:** Go 1.22+, `net/http` + `github.com/go-chi/chi/v5`, stdlib for everything else. No DB, no Docker, no observability libs.

**Layout:**
```
cmd/server/main.go
internal/device/{registry.go, registry_test.go}
internal/metrics/{store.go, store_test.go, stats.go, stats_test.go}
internal/api/{router.go, handlers.go, handlers_test.go, response.go, middleware.go}
devices.csv, openapi.json, go.mod, Makefile, .golangci.yml, README.md, WRITEUP.md
```

**Per-device state:**
```go
type DeviceMetrics struct {
    heartbeatMinutes map[int64]struct{}  // unix minute → presence (dedup natural)
    uploadCount      int64
    uploadTotalNs    int64
    mu               sync.RWMutex
}
```

Top-level store is `map[string]*DeviceMetrics`, pre-populated at startup from the registry. Map itself is never written after init, so no global lock needed — only per-device `mu`. Note this reasoning in a comment.

**Calculations:**
- **Uptime:** `len(heartbeatMinutes) / (maxMinute - minMinute + 1) * 100`, recomputed on GET. We pick simple O(M) recompute over O(1) incremental — note the optimization in the writeup.
- **Avg upload:** `time.Duration(uploadTotalNs / uploadCount).String()` — produces `"5m10s"`, `"250ms"`, etc. When `count == 0` → `"0s"`.
- **Empty-state GET:** device exists with no data → 200 with `{"uptime": 0, "avg_upload_time": "0s"}`. Don't return 204.

**Heartbeat dedup:** truncate `sent_at` to unix minute (`t.Unix() / 60`), insert into set.

**Concurrency:** per-device `RWMutex`. Writes `Lock()`, GET `RLock()`. No global hot-path lock.

**Errors (per OpenAPI):**
- Unknown device → `404` `{"msg": "device not found"}`
- Malformed body / decode failure → `500` `{"msg": "..."}` (deliberate spec conformance over HTTP-correct 400; note in writeup)
- Successful POSTs → `204` empty body
- All errors via shared `writeError(w, status, msg)` helper

**Middleware:** chi `RequestID`, `Logger`, `Recoverer`.

## Don't write defensive slop

A common AI failure mode is adding speculative checks, redundant validation, and "just in case" branches that don't pull weight. Don't do this. Specifically:

- **No nil checks on values that can't be nil** by construction (e.g., struct fields populated in a constructor, parameters from a non-nil caller).
- **No re-validation of data already validated upstream.** If the router has matched `{device_id}`, the path param exists — don't check for empty string. If JSON decoding succeeded into a typed struct, the field has the declared type.
- **Don't catch errors you can't meaningfully handle.** Propagate with `%w` wrap. Don't log-and-continue.
- **No "shouldn't happen" branches with elaborate fallbacks.** If it shouldn't happen, a panic on that path is correct — the recovery middleware turns it into a 500.
- **No comments that restate the code.** `// increment count` above `count++` is noise. Comments explain *why* or flag non-obvious context.
- **No defensive copying** of slices/maps unless there's a real aliasing risk you can name.
- **No try/recover for control flow.** Errors are values; use them.
- **No premature abstraction.** No interfaces without a real second implementation or test seam. No "for future flexibility" parameters.
- **No god-helpers.** A function that takes 6 booleans and switches on them is a smell. Write the two functions.

If you find yourself adding a check and you can't articulate the concrete failure it prevents, delete it.

## No workarounds on errors

When something fails — a test, a build, a library call, a tool — investigate the cause before patching around it. Iterating workarounds on top of unexplained failures is how spaghetti gets made.

- **Don't modify or vendor a third-party library to "fix" it.** If chi, the stdlib, or any dep behaves unexpectedly, the cause is almost always our code, our version, or our understanding — not the library. Read the docs, read the source if needed, then fix the call site.
- **Don't add try/recover, retries, or fallback paths to silence an error you haven't diagnosed.** An unexplained error is a signal, not noise. Find the cause first.
- **Don't pile fixes on top of fixes.** If your second attempt at a problem starts with "and also handle the case where…", stop. Revert to before the first fix and re-diagnose. The original mental model was probably wrong.
- **Don't change the test to make it pass.** If a test fails, the production code is wrong, the test was wrong from the start, or the spec changed. Identify which one and fix it deliberately. Never edit assertions to match buggy output.
- **Don't suppress lint or vet warnings without a one-line reason.** `//nolint:errcheck // intentional, see X` is acceptable. `//nolint` alone is not.
- **When stuck, ask.** It's better to surface "I don't understand why X is happening, here's what I've tried" than to ship five layers of workaround. I'd rather review a diagnostic question than untangle accumulated patches.

Heuristic: if your fix makes the code *more complicated* without you being able to explain *exactly* what was broken, the fix is probably wrong.

## TDD discipline

- **Red → green → refactor.** No production code without a failing test driving it.
- **One test at a time** (or one table-driven group). Run `go test`, see it fail with the expected message, then implement.
- **Refactor only on green.** If a refactor breaks a test, revert or fix forward — don't pile on changes.
- **Test names describe behavior**, not implementation: `TestRegistry_RejectsUnknownDevice`, not `TestHasReturnsFalse`.
- **Run `go test -race ./...`** before completing each phase.

**Use `TEST_CASES.md` as your reference.** Each time you're ready to write tests for a function or handler:
1. Open `TEST_CASES.md` and find the relevant section.
2. Pick the next case (or the next group of related cases for a table-driven test).
3. Write that test, see it fail, implement, see it pass.
4. Continue down the list. Skip cases that don't apply given your implementation choices, but document the skip in a comment.

The cases in `TEST_CASES.md` are a starting point, not a contract. Combine, adapt, or extend them — but cover the surface area they describe.

## Phased build with checkpoints

**Stop at the end of each phase. Show me what you built. Wait for review.**

### Phase 1 — Domain layer (registry + store + stats)

1. `go mod init` (ask me for the module path).
2. Read `openapi.json`, `devices.csv`, and `TEST_CASES.md`. Confirm understanding in 3-5 bullets before writing code.
3. TDD `internal/device/registry.go` against the Registry section of `TEST_CASES.md`.
4. TDD `internal/metrics/store.go` against the Store section, including concurrency tests.
5. TDD `internal/metrics/stats.go` (pure functions) against the Stats section, table-driven.
6. Run `go test -race ./internal/...`. All green.

**Stop. Show file tree, test output, race-clean confirmation. Wait for review.**

### Phase 2 — HTTP layer

1. `internal/api/response.go`: `writeError`, `writeJSON` helpers. Test these directly.
2. TDD `internal/api/handlers.go` against the Handlers section, using `httptest.NewRecorder`. Handlers depend on `*device.Registry` and `*metrics.Store` injected via a `Handlers` struct.
3. `internal/api/router.go`: chi router, `/api/v1` group, middleware (RequestID, Logger, Recoverer). Test routing rules per `TEST_CASES.md`.
4. `cmd/server/main.go`: `-port` and `-csv` flags, load registry, init store, init handlers, serve. Graceful shutdown on SIGINT/SIGTERM via `signal.NotifyContext` + `http.Server.Shutdown`. Brief comment on why.
5. Run `go test -race ./...`. All green.

**Stop. Show test output and a `curl` smoke test against a running server (one of each endpoint). Wait for review.**

### Phase 2.5 — Simulator integration

The unit and integration tests verify our *interpretation* of the spec. The simulator is the ground truth — it can reveal mismatches that no test we wrote can catch (timestamp parsing quirks, duration string formatting, header expectations, path prefix issues, etc.).

This phase has two parts: prep work you do, and an iteration loop we run together if needed.

**Prep work in this phase:**

1. **Request body logging middleware** (`internal/api/middleware.go`):
   - Custom middleware that logs the request body for `POST /devices/*` paths.
   - Gated by a `-debug` flag passed through from `main.go` (default off).
   - Reads the body, logs it, then restores `r.Body` so the handler can read it again (idiom: `io.NopCloser(bytes.NewReader(bodyBytes))`).
   - Keep it minimal — `method path status body` is enough.
2. **Health endpoint:** `GET /healthz` returns 200 with body `{"status": "ok"}`. Outside `/api/v1`, no auth, no device lookup. Three lines, useful for verifying the server is up before pointing the simulator at it.
3. **Debug flag wiring:** `-debug` flag in `main.go` toggles the body-logging middleware. README documents both the flag and `/healthz`.

**Then stop.** Ali will:

1. Download the simulator binary for his platform.
2. Start the server with `-debug` enabled.
3. Verify with `curl http://localhost:6733/healthz`.
4. Run the simulator pointing at port 6733.
5. Capture the simulator's output.

**Iteration loop (if simulator reports failures):**

If the simulator reports any device mismatches, Ali will paste the failure output back along with the relevant server log lines. Your job:

- **Diagnose first, patch second.** Identify *which assumption* in our implementation is wrong before changing code. Common suspects: timestamp parsing format, duration string output, response body shape, path prefix, device ID case sensitivity.
- **Add a regression test** for the misunderstanding before fixing. The new test should fail in the same way the simulator did. Then fix the code, see both green.
- **Don't pile fixes.** If the first fix doesn't resolve it, revert and re-diagnose. Workarounds on top of workarounds is the failure mode this prompt explicitly forbids.

When the simulator passes cleanly, capture `results.txt` and proceed to Phase 3.

**Stop after prep work. Wait for Ali to run the simulator and report back.**

### Phase 3 — Tooling and docs

1. `Makefile`: `run`, `test`, `test-race`, `lint`, `tidy`, `build`. One line each.
2. `.golangci.yml`: `errcheck`, `govet`, `staticcheck`, `revive`, `gofmt`, `goimports`. Don't enable everything.
3. `README.md`: summary, quickstart (`make run`, `make test`), how to run the simulator (use the actual command Ali validated in Phase 2.5), `-debug` flag, `/healthz` endpoint, layout, endpoint summary linking `openapi.json`.
4. `WRITEUP.md` — required and optional sections:
   - **Time spent + hardest part** — `[FILL IN]` placeholder
   - **Extending the data model:** sketch a `MetricAggregator` interface (`Record(value, t)`, `Snapshot() any`); per-device `map[string]MetricAggregator`. Note we deliberately did *not* build this now — premature abstraction — but the refactor path is clear.
   - **Complexity:** insert O(1), GET O(M) where M = distinct active minutes; mention O(1) GET via `firstMinute`/`lastMinute` tracking on insert
   - **AI tool usage:** Used Claude (Claude Code) for scaffolding and Go idiom review; wrote and validated logic and tests via TDD. `[Ali — adjust to match what actually happened.]`
   - **Production caveats:** in-memory state lost on restart (mitigation: time-series store or Postgres `(device_id, minute_bucket)` index); `int64` ns sum overflow (~290 yrs cumulative; mitigation: windowed aggregate or HDR histogram); single-process scaling (mitigation: queue-fronted ingestion + Redis hot aggregates)
   - **Spec deviation:** 500 for malformed bodies because OpenAPI only enumerates 404/500; HTTP-correct would be 400. Conformance chosen over correctness because the simulator is the test.
   - **Security/testing/deployment (brief):** mTLS or API keys for edge devices behind an API gateway; fuzz-test body parsing and load-test the heartbeat hot path before rollout; multi-stage Dockerfile + LB once state is externalized; health endpoint, JSON logs, Prometheus latency + per-device active-count metrics as next adds.

**Stop. Show final tree. Ask if I want adjustments.**

## Style guidance

- **Comments:** explain *why*, not *what*. One-line Go-idiom callouts welcome for the beginner audience.
- **Errors:** wrap with `fmt.Errorf("loading registry: %w", err)`. Don't swallow.
- **No globals.** DI through constructors. Note the pattern in a comment when it first appears.
- **Naming:** short, idiomatic. One/two-letter receivers. `id` not `deviceID` inside a method on a `Device`.
- **Imports:** stdlib, blank line, third-party. `goimports`-formatted.

## What NOT to do

- No DB (not even SQLite), no Docker, no CI, no observability libs (mention them in writeup, don't install).
- No code generators against the OpenAPI spec — write handlers by hand.
- No `MetricAggregator` interface in code.
- No `panic` for control flow.
- No `interface{}`/`any` outside genuine JSON helpers.
- No filler tests (trivial getters, redundant cases).

## Begin

Phase 1, step 1: ask me for the module path. Then read `openapi.json`, `devices.csv`, and `TEST_CASES.md` and confirm understanding in 3-5 bullets before writing any code.
