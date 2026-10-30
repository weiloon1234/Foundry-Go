//go:build unix

package devserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) func(canceled bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return func(canceled bool) {
		if canceled && cmd.Process != nil && cmd.Process.Pid > 0 {
			// WaitDelay bounds an uncooperative child. Reap any remaining processes
			// in its group before returning ownership to the caller.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
}

func notifyHangup(ctx context.Context) (context.Context, context.CancelFunc) {
	if !signal.Ignored(syscall.SIGHUP) {
		return signal.NotifyContext(ctx, syscall.SIGHUP)
	}
	return ctx, func() {}
}
