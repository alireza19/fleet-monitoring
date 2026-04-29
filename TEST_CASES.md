# Test Cases Reference

This document enumerates the test cases for each package. Use these as a starting
point when writing tests in TDD style — but you are not bound to write exactly
these. Combine related cases into table-driven groups where natural, skip cases
that don't make sense given an implementation choice you've made (and document
why), and add cases you discover are needed during implementation.

The goal is **meaningful coverage**, not test count. Don't pad.

---

## `internal/device` — Registry

### Loading

1. **Loads a single device from a valid CSV** — header + one data row → registry has that ID.
2. **Loads many devices** — header + N data rows → all N IDs present, lookups return true.
3. **Skips header row** — first row `device_id` is not loaded as a device.
4. **Empty file (header only)** — registry is empty, no error.
5. **Missing file** — returns wrapped error (not panic), error mentions the path.
6. **Malformed CSV** (e.g., unclosed quote) — returns wrapped error.
7. **Duplicate IDs in CSV** — loaded once, no error (idempotent).
8. **Whitespace around IDs** — trimmed on load. Document this in a comment as defensive against CSV quirks from spreadsheet exports.
9. **Empty/blank rows** — skipped, no error.

### Lookup

10. **Known ID → true.**
11. **Unknown ID → false.**
12. **Empty string → false.**
13. **Case sensitivity** — `"Device-1"` and `"device-1"` are distinct. Document this is the chosen behavior (matching CSV exactly, no normalization).

---

## `internal/metrics` — Store

### Heartbeat recording

14. **Single heartbeat** — snapshot shows 1 active minute.
15. **Same-minute dedup** — two heartbeats at `14:23:01` and `14:23:59` → 1 active minute.
16. **Distinct minutes** — heartbeats at `14:23` and `14:24` → 2 active minutes.
17. **Out-of-order arrival** — record `14:25` then `14:23` → both present; min/max correct on snapshot.
18. **Far-apart heartbeats** — only `14:00` and `14:09` recorded → 2 active minutes, span = 10.
19. **Timezone equivalence** — same wall moment expressed in different timezones (UTC vs PST) → same minute bucket. (Verifies we operate on `time.Time.Unix()`, which is timezone-agnostic.)

### Upload recording

20. **Single upload** — record 100ns → count=1, total=100.
21. **Multiple uploads** — record 100, 200, 300 → count=3, total=600.
22. **Zero upload time** — accepted (don't reject; spec doesn't forbid).
23. **Large upload values** — record several uploads near `int64` max / N to confirm no naive overflow in the running sum within realistic ranges.

### Independence

24. **Heartbeats only** — no uploads recorded → snapshot has minutes, count=0, total=0.
25. **Uploads only** — no heartbeats → snapshot has zero minutes, correct upload aggregates.
26. **Multiple devices isolated** — recording for device A doesn't affect device B's snapshot.

### Concurrency (run with `-race`)

27. **Concurrent heartbeats, same device** — N goroutines × M heartbeats across a fixed minute window → final count ≤ window size, no race.
28. **Concurrent uploads, same device** — N goroutines × M uploads of value 1 → final count = N×M, total = N×M, no race.
29. **Concurrent reads + writes, same device** — one goroutine snapshotting in a loop while another writes heartbeats and uploads → no panic, no race, snapshots are internally consistent (no torn reads where count and total disagree).
30. **Concurrent activity across devices** — many goroutines each writing to a different device → all devices have correct final state, no cross-contamination.

---

## `internal/metrics` — Stats (pure functions, table-driven)

### Uptime calculation

| Case | Input | Expected |
|------|-------|----------|
| 31 | Zero heartbeats | 0.0 |
| 32 | Single heartbeat | 100.0 (span = 1, count = 1) |
| 33 | Full coverage of 10-minute span | 100.0 |
| 34 | 6 of 10 minutes (spec example) | 60.0 |
| 35 | 2 of 10 minutes (sparse) | 20.0 |
| 36 | All heartbeats in same minute | 100.0 (span = 1) |
| 37 | First minute 0, last minute 999, 500 minutes recorded | 50.05 (float precision sanity) |
| 38 | First minute 0, last minute 1, 1 minute recorded | 50.0 |

### Avg upload formatting

| Case | Input (totalNs, count) | Expected |
|------|-------|----------|
| 39 | (0, 0) | `"0s"` |
| 40 | (500_000_000, 1) | `"500ms"` |
| 41 | (1_000_000_000, 1) | `"1s"` |
| 42 | (1_500_000_000, 1) | `"1.5s"` |
| 43 | (310_000_000_000, 1) | `"5m10s"` (spec literal example) |
| 44 | (150, 1) | `"150ns"` |
| 45 | (1500, 1) | `"1.5µs"` |
| 46 | (10, 3) | `"3ns"` (integer division truncates; document this) |
| 47 | (300, 2) | `"150ns"` (clean division) |

---

## `internal/api` — Handlers (httptest)

### POST `/devices/{id}/heartbeat`

48. **Happy path** — known device, valid body → 204, empty response body.
49. **Unknown device** → 404 with `{"msg": "device not found"}`.
50. **Malformed JSON** (body `{`) → 500 with `{"msg": "..."}`.
51. **Missing required field** (body `{}`) → 500.
52. **Wrong field type** (`sent_at` is integer) → 500.
53. **Invalid timestamp format** (`sent_at: "not-a-date"`) → 500.
54. **Empty body** → 500.
55. **Side-effect verification** — POST heartbeat, then GET stats, uptime > 0.

### POST `/devices/{id}/stats`

56. **Happy path** — known device, valid body with `upload_time: 5000000000` → 204.
57. **Unknown device** → 404.
58. **Malformed JSON** → 500.
59. **Missing `upload_time`** → 500.
60. **Missing `sent_at`** → 500.
61. **Wrong field type** (`upload_time` as string) → 500.
62. **Zero `upload_time`** → 204, recorded as zero.
63. **Negative `upload_time`** — design call: accepted (spec doesn't forbid). Test verifies current behavior; document the choice inline.
64. **Side-effect verification** — POST stats, then GET, avg_upload_time reflects the upload.

### GET `/devices/{id}/stats`

65. **Empty state** — known device, no data → 200 with `{"uptime": 0, "avg_upload_time": "0s"}`.
66. **Unknown device** → 404.
67. **Heartbeats only** — record 6 heartbeats over a 10-minute span → 200 with uptime=60, avg="0s".
68. **Stats only** — record 3 uploads of 100ms each → 200 with uptime=0, avg="100ms".
69. **Both populated** — heartbeats + stats → 200 with both fields correct.
70. **Response shape** — `uptime` is JSON number (not string), `avg_upload_time` is JSON string.
71. **Content-Type header** — response carries `Content-Type: application/json`.
72. **Realistic flow** — simulate a 10-minute device session: 8 heartbeats spread across the window + 3 uploads with varying times → final GET matches expected calculations end-to-end.

### Routing and middleware

73. **Wrong method** — `GET /devices/{id}/heartbeat` (only POST defined) → 405.
74. **Unknown path** — `POST /devices/{id}/unknown` → 404.
75. **Missing `/api/v1` prefix** — `POST /devices/{id}/heartbeat` without prefix → 404.
76. **Panic recovery** — a deliberately panicking test handler → 500, server stays up. Wire a temporary route in the test only.

---

## Notes on table-driven grouping

When you implement these, group naturally:

- Stats math (cases 31–47) → two table-driven test functions: `TestUptime` and `TestAvgUploadFormat`.
- Per-handler error cases (50–54, 58–61) → one table-driven test per handler covering all malformed-input variants.
- Routing cases (73–75) → one table-driven test.

Concurrency tests (27–30) cannot be table-driven cleanly — keep them as separate functions.

## Notes on cases you may legitimately skip

- If your handler implementation makes some of the malformed-body cases indistinguishable (e.g., your decoder treats missing field and wrong type identically), collapse those into a single table row rather than fabricating a distinction.
- If you choose stricter behavior on a design call (e.g., reject negative `upload_time`), update the corresponding test to assert the rejection and note the deviation.

Document any case you skip and why in a comment near where the test would have lived.
