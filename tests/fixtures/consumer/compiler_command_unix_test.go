//go:build unix

package consumer_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// The go command starts compiler children. Cancel its owned process group so a
// timed-out negative example cannot leave compilation running in the background.
func configureCompilerCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
}

func TestCompilerCancellationClosesDescendantPipes(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("process-tree regression requires an existing POSIX shell")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", "sleep 30 & printf 'ready\\n'; wait")
	configureCompilerCommand(cmd)
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	input := bufio.NewReader(reader)
	if ready, err := input.ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatal("descendant did not start", err)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("canceled process reported success")
	}
	if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// This pipe belongs to the test, not Cmd.StdoutPipe: Wait cannot close its
	// reader to hide a surviving descendant that still holds the write end.
	if _, err := io.ReadAll(input); err != nil {
		t.Fatal("cancellation left a descendant holding the output pipe", err)
	}
}
