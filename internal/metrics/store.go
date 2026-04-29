// Package metrics owns per-device metric aggregation: heartbeat minute
// buckets and upload duration sums. The store is in-memory only and
// initialized from a fixed device list at startup.
package metrics

import (
	"sync"
	"time"
)

// Store holds metrics for the entire device fleet. The top-level map is
// populated once at construction and never modified afterward, so reads
// from it (a `map[string]*deviceMetrics` lookup) need no synchronization
// — only per-device mutations require locking.
type Store struct {
	devices map[string]*deviceMetrics
}

// deviceMetrics holds one device's state behind an RWMutex. Lowercase: the
// type is an internal implementation detail not exposed across the package
// boundary. Reads (Snapshot) take RLock; writes take Lock.
type deviceMetrics struct {
	mu               sync.RWMutex
	heartbeatMinutes map[int64]struct{}
	uploadCount      int64
	uploadTotalNs    int64
}

// Snapshot is a value-typed read-side view returned by Store.Snapshot. It
// is taken under RLock and returned by value, so callers cannot mutate
// store state through it. Fields are public so the stats package can
// project them into the GET response.
//
// FirstMinute and LastMinute are valid only when ActiveMinutes > 0.
type Snapshot struct {
	ActiveMinutes int64
	FirstMinute   int64
	LastMinute    int64
	UploadCount   int64
	UploadTotalNs int64
}

// NewStore pre-allocates a deviceMetrics entry for each known ID. Pre-allocation
// at startup means the top-level map is read-only thereafter and we avoid any
// global lock on the hot path — every recorded event hits a single device's
// per-mutex.
func NewStore(deviceIDs []string) *Store {
	devs := make(map[string]*deviceMetrics, len(deviceIDs))
	for _, id := range deviceIDs {
		devs[id] = &deviceMetrics{
			heartbeatMinutes: make(map[int64]struct{}),
		}
	}
	return &Store{devices: devs}
}

// RecordHeartbeat marks the unix-minute corresponding to t as active for the
// given device. Same-minute heartbeats collapse via map insertion (set
// semantics); we deliberately do not track count-per-minute since the spec
// only cares about presence.
//
// Precondition: caller must verify the device exists via Registry.Has before
// calling. Passing an unknown id will panic on the nil-pointer dereference
// of s.devices[id]; the recovery middleware turns that into a 500.
func (s *Store) RecordHeartbeat(id string, t time.Time) {
	d := s.devices[id]
	// t.Unix() is timezone-agnostic (seconds since epoch). Dividing by 60
	// gives the minute bucket, which is what the spec asks us to dedup on.
	minute := t.Unix() / 60
	d.mu.Lock()
	defer d.mu.Unlock()
	d.heartbeatMinutes[minute] = struct{}{}
}

// RecordUpload appends one upload sample to the running aggregates.
//
// Precondition: caller must verify the device exists via Registry.Has before
// calling. Passing an unknown id will panic; the recovery middleware turns
// that into a 500.
func (s *Store) RecordUpload(id string, durationNs int64) {
	d := s.devices[id]
	d.mu.Lock()
	defer d.mu.Unlock()
	d.uploadCount++
	d.uploadTotalNs += durationNs
}

// Snapshot returns a point-in-time view of one device's state. Computing
// first/last minute by iteration is O(M) where M is the number of distinct
// active minutes — a deliberate choice over O(1) incremental tracking. M
// is bounded by session length and the recompute cost is paid only on GET.
//
// Precondition: caller must verify the device exists via Registry.Has before
// calling. Passing an unknown id will panic; the recovery middleware turns
// that into a 500.
func (s *Store) Snapshot(id string) Snapshot {
	d := s.devices[id]
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap := Snapshot{
		ActiveMinutes: int64(len(d.heartbeatMinutes)),
		UploadCount:   d.uploadCount,
		UploadTotalNs: d.uploadTotalNs,
	}
	if snap.ActiveMinutes > 0 {
		first, last := int64(1<<63-1), int64(-1<<63)
		for m := range d.heartbeatMinutes {
			if m < first {
				first = m
			}
			if m > last {
				last = m
			}
		}
		snap.FirstMinute = first
		snap.LastMinute = last
	}
	return snap
}
