package device

import (
	"path/filepath"
	"testing"
)

// writeCSV is a test helper that writes content to a temp file and returns its
// path. Idiomatic Go: helpers live in *_test.go and call t.Helper() so failure
// lines point at the caller, not the helper.
func writeCSV(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir() // auto-cleaned at end of test
	path := filepath.Join(dir, "devices.csv")
	if err := writeFile(path, content); err != nil {
		t.Fatalf("writing temp csv: %v", err)
	}
	return path
}

func TestRegistry_LoadsSingleDevice(t *testing.T) {
	path := writeCSV(t, "device_id\nabc-123\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if !reg.Has("abc-123") {
		t.Errorf("Has(\"abc-123\") = false, want true")
	}
}

func TestRegistry_EmptyFileHeaderOnly(t *testing.T) {
	path := writeCSV(t, "device_id\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if reg.Has("device_id") {
		t.Errorf("header row was loaded as a device id")
	}
}

func TestRegistry_LookupUnknown(t *testing.T) {
	path := writeCSV(t, "device_id\nabc-123\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if reg.Has("nope") {
		t.Errorf("Has(\"nope\") = true, want false")
	}
}

// TestRegistry_LoadVariants groups CSV-shape edge cases that all assert the
// same kind of thing: load returns no error and the resulting registry has
// the expected set of IDs. Table-driven keeps related cases readable as a
// single block.
func TestRegistry_LoadVariants(t *testing.T) {
	tests := []struct {
		name     string
		csv      string
		wantHave []string
		wantMiss []string
	}{
		{
			name:     "many devices",
			csv:      "device_id\na\nb\nc\nd\ne\n",
			wantHave: []string{"a", "b", "c", "d", "e"},
		},
		{
			name:     "header is not loaded as device",
			csv:      "device_id\nreal-id\n",
			wantHave: []string{"real-id"},
			wantMiss: []string{"device_id"},
		},
		{
			name:     "duplicate ids are deduped",
			csv:      "device_id\nx\nx\nx\n",
			wantHave: []string{"x"},
		},
		{
			// Trim whitespace defensively — spreadsheet exports often introduce
			// stray spaces around values that the human eye doesn't see.
			name:     "whitespace around ids is trimmed",
			csv:      "device_id\n  spaced-id  \n\ttabbed-id\t\n",
			wantHave: []string{"spaced-id", "tabbed-id"},
			wantMiss: []string{"  spaced-id  ", "\ttabbed-id\t"},
		},
		{
			name:     "blank rows are skipped",
			csv:      "device_id\n\nfoo\n\n\nbar\n",
			wantHave: []string{"foo", "bar"},
			wantMiss: []string{""},
		},
		{
			// Lookup is case-sensitive. We deliberately do not normalize:
			// the registry mirrors the CSV exactly so simulator IDs must
			// match how they were provisioned.
			name:     "case sensitivity preserved",
			csv:      "device_id\nDevice-1\n",
			wantHave: []string{"Device-1"},
			wantMiss: []string{"device-1", "DEVICE-1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCSV(t, tc.csv)
			reg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			for _, id := range tc.wantHave {
				if !reg.Has(id) {
					t.Errorf("Has(%q) = false, want true", id)
				}
			}
			for _, id := range tc.wantMiss {
				if reg.Has(id) {
					t.Errorf("Has(%q) = true, want false", id)
				}
			}
		})
	}
}

func TestRegistry_MissingFile(t *testing.T) {
	_, err := Load("/no/such/path/devices.csv")
	if err == nil {
		t.Fatal("Load: expected error for missing file, got nil")
	}
	// Error must mention the path so operators can diagnose without a stack
	// trace. Substring check rather than exact match — the wrapping message
	// is allowed to evolve.
	if !contains(err.Error(), "/no/such/path/devices.csv") {
		t.Errorf("error %q does not mention path", err.Error())
	}
}

func TestRegistry_MalformedCSV(t *testing.T) {
	// An unterminated quoted field is the canonical csv parser error.
	path := writeCSV(t, "device_id\n\"unterminated\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error for malformed CSV, got nil")
	}
}

func TestRegistry_IDsReturnsAllLoaded(t *testing.T) {
	path := writeCSV(t, "device_id\nalpha\nbeta\ngamma\n")
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := reg.IDs()
	if len(got) != 3 {
		t.Fatalf("len(IDs()) = %d, want 3", len(got))
	}
	// Iteration order over a map is unspecified; build a set for the assertion.
	set := map[string]bool{}
	for _, id := range got {
		set[id] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !set[want] {
			t.Errorf("IDs() missing %q (got %v)", want, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
