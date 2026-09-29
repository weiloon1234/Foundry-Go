//go:build darwin || linux

package filelock

import (
	"errors"
	"os"
	"syscall"
)

// Try takes an exclusive lock only when it is immediately available.
func Try(file *os.File) (bool, error) { return try(file, exclusive) }

func operation(m mode) int {
	if m == shared {
		return syscall.LOCK_SH
	}
	return syscall.LOCK_EX
}
func try(file *os.File, m mode) (bool, error) {
	err := syscall.Flock(int(file.Fd()), operation(m)|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
		return false, nil
	}
	return err == nil, err
}
