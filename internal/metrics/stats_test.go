package metrics

import (
	"math"
	"testing"
)

func TestUptime(t *testing.T) {
	tests := []struct {
		name          string
		activeMinutes int64
		firstMinute   int64
		lastMinute    int64
		want          float64
	}{
		{name: "zero heartbeats", activeMinutes: 0, want: 0.0},
		// last == first triggers the single-distinct-minute special case.
		{name: "single heartbeat", activeMinutes: 1, firstMinute: 100, lastMinute: 100, want: 100.0},
		// "10-minute span" under the simulator's formula means last-first=10.
		// Originally written with last=9 to match the (max-min+1) formula;
		// updated in Phase 2.5 along with the production fix.
		{name: "full coverage of 10-minute span", activeMinutes: 10, firstMinute: 0, lastMinute: 10, want: 100.0},
		{name: "6 of 10-minute span (spec example)", activeMinutes: 6, firstMinute: 0, lastMinute: 10, want: 60.0},
		{name: "2 of 10-minute span (sparse)", activeMinutes: 2, firstMinute: 0, lastMinute: 10, want: 20.0},
		{name: "same minute", activeMinutes: 1, firstMinute: 42, lastMinute: 42, want: 100.0},
		// Float-precision sanity: 500 / 999 * 100 = 50.0500500500... TEST_CASES.md
		// case 37 originally expected 50.05 with last=999; under the new (max-min)
		// formula those inputs give that result directly, no adjustment needed.
		{name: "non-trivial fraction", activeMinutes: 500, firstMinute: 0, lastMinute: 999, want: 50.05005005005005},
		// 2 distinct minutes spanning a 4-minute interval (e.g., minute 0 and
		// minute 4 with 1, 2, 3 missed): 2/4 = 50%.
		{name: "ratio one half", activeMinutes: 2, firstMinute: 0, lastMinute: 4, want: 50.0},
		// Phase 2.5 simulator regression: device 18-b8-87-e7-1f-06 sent 474
		// heartbeats over a 480-minute window. Simulator expected 98.75
		// (= 474/480). Our original (max-min+1) formula computed 474/481 ≈
		// 98.5447, off by ~0.21pp. Captured before touching production code so
		// the failure shape matches what the simulator reported.
		{name: "simulator regression: 474 of 480-minute window", activeMinutes: 474, firstMinute: 0, lastMinute: 480, want: 98.75},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Uptime(tc.activeMinutes, tc.firstMinute, tc.lastMinute)
			// Compare with a small epsilon since 500/999*100 isn't exactly
			// representable in float64. 1e-9 is generous for double precision.
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Uptime(%d, %d, %d) = %v, want %v",
					tc.activeMinutes, tc.firstMinute, tc.lastMinute, got, tc.want)
			}
		})
	}
}

func TestAvgUploadString(t *testing.T) {
	tests := []struct {
		name    string
		totalNs int64
		count   int64
		want    string
	}{
		{name: "zero count returns 0s", totalNs: 0, count: 0, want: "0s"},
		{name: "500ms", totalNs: 500_000_000, count: 1, want: "500ms"},
		{name: "1s", totalNs: 1_000_000_000, count: 1, want: "1s"},
		{name: "1.5s", totalNs: 1_500_000_000, count: 1, want: "1.5s"},
		// Spec example literal
		{name: "5m10s", totalNs: 310_000_000_000, count: 1, want: "5m10s"},
		{name: "150ns", totalNs: 150, count: 1, want: "150ns"},
		{name: "1.5µs", totalNs: 1500, count: 1, want: "1.5µs"},
		// Integer division truncates: 10ns / 3 = 3ns. Documented behavior —
		// we don't round, since avg upload time is a coarse fleet-health
		// signal and sub-ns precision isn't meaningful.
		{name: "truncating division", totalNs: 10, count: 3, want: "3ns"},
		{name: "clean division", totalNs: 300, count: 2, want: "150ns"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AvgUploadString(tc.totalNs, tc.count)
			if got != tc.want {
				t.Errorf("AvgUploadString(%d, %d) = %q, want %q",
					tc.totalNs, tc.count, got, tc.want)
			}
		})
	}
}
