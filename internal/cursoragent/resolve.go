// Package cursoragent resolves the Kilroy @cursor/sdk CLI bridge.
package cursoragent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// EnvPath overrides the cursor-agent executable (wrapper script or node entry).
	EnvPath = "KILROY_CURSOR_AGENT_PATH"
	// DefaultExecutable is the shell wrapper name on PATH when not resolved absolutely.
	DefaultExecutable = "kilroy-cursor-agent"
)

// ResolveExecutable returns the path to the kilroy-cursor-agent wrapper or override.
func ResolveExecutable() string {
	if v := strings.TrimSpace(os.Getenv(EnvPath)); v != "" {
		return v
	}
	// Relative to this source tree (development).
	_, file, _, ok := runtime.Caller(0)
	if ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
		wrapper := filepath.Join(root, "scripts", "kilroy-cursor-agent")
		if _, err := os.Stat(wrapper); err == nil {
			return wrapper
		}
	}
	return DefaultExecutable
}
