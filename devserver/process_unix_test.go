//go:build unix

package devserver

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
)

func TestProcessGroupCleanup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ending   string
		cancel   bool
		exitCode int
	}{
		{name: "successful leader exit", ending: "exit 0"},
		{name: "failed leader exit", ending: "exit 7", exitCode: 7},
		{name: "cancellation", ending: "wait", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			wait, err := startChild(ctx, t.TempDir(), "sh", []string{
				"-c", "sleep 30 & printf '%s\\n' \"$!\"; " + tc.ending,
			}, nil, cli.Streams{Out: writer, Err: writer})
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			input := bufio.NewReader(reader)
			line, err := input.ReadString('\n')
			if err != nil {
				t.Fatalf("descendant did not start: %v", err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid <= 0 {
				t.Fatalf("invalid descendant PID %q", line)
			}
			// Always clean up the test-owned descendant, including before the fix.
			defer syscall.Kill(pid, syscall.SIGKILL)
			if tc.cancel {
				cancel()
			}
			err = wait()
			if tc.cancel {
				if err == nil {
					t.Fatal("canceled child reported success")
				}
			} else if tc.exitCode == 0 {
				if err != nil {
					t.Fatalf("successful leader exit: %v", err)
				}
			} else {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.exitCode {
					t.Fatalf("wait = %v, want exit code %d", err, tc.exitCode)
				}
			}
			if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			// Unlike Cmd.StdoutPipe, this pipe cannot be closed by Wait to hide
			// a live descendant that still owns its write end.
			if _, err := io.ReadAll(input); err != nil {
				t.Fatalf("leader completion left a descendant holding the output pipe: %v", err)
			}
		})
	}
}
