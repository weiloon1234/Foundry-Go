//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package generate

import (
	"fmt"
	"os"
	"path/filepath"
)

const automaticRecovery = false

// Normal generation retains an exclusive directory guard on platforms without
// the implemented advisory-lock adapter. Crash recovery needs manual inspection
// there; it must never guess whether another process is alive.
func acquireGuard(dir string) (func(), error) {
	path := filepath.Join(dir, ".foundry-generate.guard-directory")
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, fmt.Errorf("acquire generation guard (inspect stale guards on this platform): %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}
