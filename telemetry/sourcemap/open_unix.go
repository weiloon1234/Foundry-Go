//go:build unix

package sourcemap

import (
	"os"
	"syscall"
)

func openRegular(root *os.Root, name string) (*os.File, error) {
	// A swapped FIFO must not block; a swapped symlink must never be followed.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
