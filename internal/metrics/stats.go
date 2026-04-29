package metrics

import "time"

// Uptime returns the percentage of distinct minutes during which the device
// was active across the observed span. Inputs come from a Snapshot.
//
// Span is `last - first` (the elapsed minutes between the first and last
// observed heartbeat), not `last - first + 1`. The original prompt
// specified the +1 form; the Phase 2.5 simulator integration revealed it
// was wrong by exactly one in the denominator across all five test devices.
// The simulator generates heartbeats over an interval where max-min equals
// its expected denominator, so we follow that contract.
//
// Edge cases:
//   - activeMinutes == 0: device has never reported; return 0.
//   - last == first (single distinct minute observed): elapsed span is 0,
//     would divide by zero. Return 100 — we observed the device for the
//     entire window we know about, however brief.
func Uptime(activeMinutes, firstMinute, lastMinute int64) float64 {
	if activeMinutes == 0 {
		return 0
	}
	span := lastMinute - firstMinute
	if span == 0 {
		return 100
	}
	return float64(activeMinutes) / float64(span) * 100
}

// AvgUploadString formats the mean upload duration as Go's idiomatic
// duration string (e.g., "5m10s", "500ms", "0s"). Zero count returns "0s"
// per the empty-state contract on GET /devices/{id}/stats.
//
// Integer division truncates: AvgUploadString(10, 3) == "3ns", not "3.33ns".
// Sub-nanosecond precision isn't meaningful for the metric and avoids
// floating-point noise in the response.
func AvgUploadString(totalNs, count int64) string {
	if count == 0 {
		return "0s"
	}
	return time.Duration(totalNs / count).String()
}
