package metrics

import (
	"sync"
	"testing"
	"time"
)

const dev = "dev-A"

func newTestStore(t *testing.T, ids ...string) *Store {
	t.Helper()
	if len(ids) == 0 {
		ids = []string{dev}
	}
	return NewStore(ids)
}

func TestStore_RecordSingleHeartbeat(t *testing.T) {
	s := newTestStore(t)
	s.RecordHeartbeat(dev, time.Date(2026, 1, 1, 14, 23, 1, 0, time.UTC))

	snap := s.Snapshot(dev)
	if snap.ActiveMinutes != 1 {
		t.Errorf("ActiveMinutes = %d, want 1", snap.ActiveMinutes)
	}
}

// TestStore_HeartbeatDedupAndSpan groups all heartbeat-bucketing scenarios
// into a single table — they all share the same shape (record N times,
// assert active count and span on snapshot).
func TestStore_HeartbeatDedupAndSpan(t *testing.T) {
	tests := []struct {
		name              string
		times             []time.Time
		wantActive        int64
		wantSpanInMinutes int64 // last - first + 1; 0 when no heartbeats
	}{
		{
			name: "same minute deduped",
			times: []time.Time{
				time.Date(2026, 1, 1, 14, 23, 1, 0, time.UTC),
				time.Date(2026, 1, 1, 14, 23, 59, 0, time.UTC),
			},
			wantActive:        1,
			wantSpanInMinutes: 1,
		},
		{
			name: "distinct adjacent minutes",
			times: []time.Time{
				time.Date(2026, 1, 1, 14, 23, 0, 0, time.UTC),
				time.Date(2026, 1, 1, 14, 24, 0, 0, time.UTC),
			},
			wantActive:        2,
			wantSpanInMinutes: 2,
		},
		{
			name: "out of order arrival tracks min and max",
			times: []time.Time{
				time.Date(2026, 1, 1, 14, 25, 0, 0, time.UTC),
				time.Date(2026, 1, 1, 14, 23, 0, 0, time.UTC),
			},
			wantActive:        2,
			wantSpanInMinutes: 3,
		},
		{
			name: "far apart heartbeats span correctly",
			times: []time.Time{
				time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC),
				time.Date(2026, 1, 1, 14, 9, 0, 0, time.UTC),
			},
			wantActive:        2,
			wantSpanInMinutes: 10,
		},
		{
			// Same wall moment in different timezones should bucket identically
			// — the store keys off t.Unix(), which is timezone-agnostic.
			name: "timezone equivalence",
			times: []time.Time{
				time.Date(2026, 1, 1, 14, 23, 0, 0, time.UTC),
				time.Date(2026, 1, 1, 6, 23, 0, 0, time.FixedZone("PST", -8*60*60)),
			},
			wantActive:        1,
			wantSpanInMinutes: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			for _, ts := range tc.times {
				s.RecordHeartbeat(dev, ts)
			}
			snap := s.Snapshot(dev)
			if snap.ActiveMinutes != tc.wantActive {
				t.Errorf("ActiveMinutes = %d, want %d", snap.ActiveMinutes, tc.wantActive)
			}
			gotSpan := snap.LastMinute - snap.FirstMinute + 1
			if snap.ActiveMinutes == 0 {
				gotSpan = 0
			}
			if gotSpan != tc.wantSpanInMinutes {
				t.Errorf("span = %d (first=%d, last=%d), want %d",
					gotSpan, snap.FirstMinute, snap.LastMinute, tc.wantSpanInMinutes)
			}
		})
	}
}

func TestStore_RecordUploads(t *testing.T) {
	tests := []struct {
		name      string
		values    []int64
		wantCount int64
		wantTotal int64
	}{
		{name: "single upload", values: []int64{100}, wantCount: 1, wantTotal: 100},
		{name: "multiple uploads", values: []int64{100, 200, 300}, wantCount: 3, wantTotal: 600},
		// Spec doesn't forbid zero — record it as-is rather than rejecting.
		{name: "zero accepted", values: []int64{0}, wantCount: 1, wantTotal: 0},
		// Three uploads near (MaxInt64 / 4) to confirm naive int64 sum doesn't
		// overflow within realistic ranges. Production caveats around long-term
		// ns sum overflow are documented in WRITEUP.md.
		{
			name:      "large values within int64 range",
			values:    []int64{2_000_000_000_000_000_000, 2_000_000_000_000_000_000, 2_000_000_000_000_000_000},
			wantCount: 3,
			wantTotal: 6_000_000_000_000_000_000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			for _, v := range tc.values {
				s.RecordUpload(dev, v)
			}
			snap := s.Snapshot(dev)
			if snap.UploadCount != tc.wantCount {
				t.Errorf("UploadCount = %d, want %d", snap.UploadCount, tc.wantCount)
			}
			if snap.UploadTotalNs != tc.wantTotal {
				t.Errorf("UploadTotalNs = %d, want %d", snap.UploadTotalNs, tc.wantTotal)
			}
		})
	}
}

func TestStore_HeartbeatsAndUploadsIndependent(t *testing.T) {
	// Heartbeats only — upload aggregates remain zero.
	s := newTestStore(t)
	s.RecordHeartbeat(dev, time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC))
	snap := s.Snapshot(dev)
	if snap.ActiveMinutes != 1 || snap.UploadCount != 0 || snap.UploadTotalNs != 0 {
		t.Errorf("heartbeats only: snapshot = %+v", snap)
	}

	// Uploads only — minute set stays empty.
	s2 := newTestStore(t)
	s2.RecordUpload(dev, 500)
	snap = s2.Snapshot(dev)
	if snap.ActiveMinutes != 0 || snap.UploadCount != 1 || snap.UploadTotalNs != 500 {
		t.Errorf("uploads only: snapshot = %+v", snap)
	}
}

func TestStore_DevicesIsolated(t *testing.T) {
	s := newTestStore(t, "A", "B")
	s.RecordHeartbeat("A", time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC))
	s.RecordUpload("A", 1000)

	snapB := s.Snapshot("B")
	if snapB.ActiveMinutes != 0 || snapB.UploadCount != 0 || snapB.UploadTotalNs != 0 {
		t.Errorf("B was contaminated by writes to A: %+v", snapB)
	}
	snapA := s.Snapshot("A")
	if snapA.ActiveMinutes != 1 || snapA.UploadCount != 1 || snapA.UploadTotalNs != 1000 {
		t.Errorf("A snapshot wrong: %+v", snapA)
	}
}

// --- Concurrency tests (run with -race) ---

func TestStore_ConcurrentHeartbeatsSameDevice(t *testing.T) {
	s := newTestStore(t)
	const goroutines = 16
	const perGoroutine = 200
	// Restrict to a 60-minute window so dedup is meaningful: ActiveMinutes
	// must end up <= 60 regardless of how many writers ran.
	const windowMinutes = 60

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				minute := int64((seed + i) % windowMinutes)
				s.RecordHeartbeat(dev, time.Unix(minute*60, 0))
			}
		}(g)
	}
	wg.Wait()

	snap := s.Snapshot(dev)
	if snap.ActiveMinutes > windowMinutes {
		t.Errorf("ActiveMinutes = %d, want <= %d", snap.ActiveMinutes, windowMinutes)
	}
	if snap.ActiveMinutes == 0 {
		t.Errorf("ActiveMinutes = 0, want > 0")
	}
}

func TestStore_ConcurrentUploadsSameDevice(t *testing.T) {
	s := newTestStore(t)
	const goroutines = 16
	const perGoroutine = 500

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				s.RecordUpload(dev, 1)
			}
		}()
	}
	wg.Wait()

	snap := s.Snapshot(dev)
	wantCount := int64(goroutines * perGoroutine)
	if snap.UploadCount != wantCount {
		t.Errorf("UploadCount = %d, want %d", snap.UploadCount, wantCount)
	}
	if snap.UploadTotalNs != wantCount {
		t.Errorf("UploadTotalNs = %d, want %d", snap.UploadTotalNs, wantCount)
	}
}

func TestStore_ConcurrentReadsAndWrites(t *testing.T) {
	s := newTestStore(t)
	stop := make(chan struct{})

	// Snapshot reader: spin and assert internal consistency.
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
				snap := s.Snapshot(dev)
				// Internal consistency: count==0 ⇔ total==0. Detects torn reads
				// where a writer increments count but hasn't yet added the
				// duration (or vice versa) without proper locking.
				if (snap.UploadCount == 0) != (snap.UploadTotalNs == 0) {
					t.Errorf("torn read: count=%d total=%d", snap.UploadCount, snap.UploadTotalNs)
					return
				}
			}
		}
	}()

	var writerWG sync.WaitGroup
	for g := 0; g < 4; g++ {
		writerWG.Add(1)
		go func(seed int) {
			defer writerWG.Done()
			for i := 0; i < 1000; i++ {
				s.RecordHeartbeat(dev, time.Unix(int64((seed+i)%30)*60, 0))
				s.RecordUpload(dev, 1)
			}
		}(g)
	}
	writerWG.Wait()
	close(stop)
	readerWG.Wait()
}

func TestStore_ConcurrentActivityAcrossDevices(t *testing.T) {
	ids := []string{"A", "B", "C", "D"}
	s := newTestStore(t, ids...)
	const perDevice = 1000

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < perDevice; i++ {
				s.RecordUpload(id, 2)
			}
		}(id)
	}
	wg.Wait()

	for _, id := range ids {
		snap := s.Snapshot(id)
		if snap.UploadCount != perDevice {
			t.Errorf("device %s: UploadCount = %d, want %d", id, snap.UploadCount, perDevice)
		}
		if snap.UploadTotalNs != 2*perDevice {
			t.Errorf("device %s: UploadTotalNs = %d, want %d", id, snap.UploadTotalNs, 2*perDevice)
		}
	}
}
