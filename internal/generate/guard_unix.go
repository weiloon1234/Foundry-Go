//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const automaticRecovery = true

// The guard inode remains in place. Removing a locked guard could let another
// process lock a different inode and publish concurrently. The OS releases this
// advisory lock when the owning process exits, including forced termination.
func acquireGuard(dir string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, ".foundry-generate.guard"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("generation guard must be a regular file")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another generator or recovery process holds this package: %w", err)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
