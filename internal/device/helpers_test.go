package device

import "os"

// writeFile is a thin wrapper over os.WriteFile used by tests to materialize
// CSV fixtures into temp dirs. Lives in a *_test.go file so it isn't compiled
// into the production binary.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
