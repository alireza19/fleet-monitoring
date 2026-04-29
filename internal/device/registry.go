// Package device owns the registry of valid device IDs loaded from a CSV
// manifest at startup. Lookups are read-only after construction, so no
// synchronization is needed once Load returns.
package device

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// Registry holds the set of valid device IDs. Built once via Load and never
// mutated afterward — DI through a constructor is the idiomatic Go pattern
// in place of package-level globals.
type Registry struct {
	ids map[string]struct{}
}

// Load reads device IDs from a CSV file and returns a Registry. The CSV is
// expected to have a header row (`device_id`) followed by one ID per line.
func Load(path string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening device registry %q: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only file, close error is uninteresting

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows; we only read column 0

	ids := make(map[string]struct{})
	first := true
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing device registry %q: %w", path, err)
		}
		if first {
			first = false
			continue // skip header row
		}
		if len(row) == 0 {
			continue
		}
		// Trim whitespace defensively: spreadsheet exports occasionally
		// introduce stray spaces around CSV values that are invisible to
		// the human who exported them.
		id := strings.TrimSpace(row[0])
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	return &Registry{ids: ids}, nil
}

// Has reports whether the given device ID is in the registry. Lookups are
// case-sensitive: we match the CSV exactly with no normalization.
func (r *Registry) Has(id string) bool {
	_, ok := r.ids[id]
	return ok
}

// IDs returns the loaded device IDs in unspecified order. Used at startup
// to seed the metrics store; not on any hot path. Returns a fresh slice so
// callers can sort or mutate without affecting the registry.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.ids))
	for id := range r.ids {
		out = append(out, id)
	}
	return out
}
