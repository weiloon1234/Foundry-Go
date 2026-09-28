//go:build unix

package http

import "syscall"

// Nonblocking open does not change regular-file reads. It prevents a raced
// FIFO replacement from blocking before the opened descriptor is checked.
const downloadOpenFlags = syscall.O_RDONLY | syscall.O_NONBLOCK
