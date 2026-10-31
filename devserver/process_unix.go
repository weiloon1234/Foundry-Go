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

func configureProcess(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return func() {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			// The group remains ours after every leader exit. WaitDelay bounds
			// waiting for the leader and inherited pipes, not descendant lifetimes.
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
