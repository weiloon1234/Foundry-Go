package filelock

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"os"
	"time"
)

// Acquire owns a distinct descriptor, serializing goroutines and processes.
// The lock file must never be removed while the root can still be in use.
func Acquire(ctx context.Context, root *os.Root, name string) (func(), error) {
	if ctx == nil || root == nil {
		return nil, fault.New(fault.Invalid, "file lock requires a context and root")
	}
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		_ = file.Close()
		return nil, fault.New(fault.Invalid, "invalid file lock")
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		locked, err := Try(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			return func() { _ = file.Close() }, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
