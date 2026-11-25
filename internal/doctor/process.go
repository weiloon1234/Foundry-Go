package doctor

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const maxOutputBytes = 64 << 10

// A named buffer prevents io.Copy from selecting an embedded ReaderFrom and
// bypassing Write's bound. Tool stderr is discarded, never replayed in reports,
// except the first line of a package listing failure (see ModulePackages).
type output struct {
	data   bytes.Buffer
	limit  int
	cancel context.CancelFunc
}

func (o *output) Write(p []byte) (int, error) {
	if len(p) > o.limit-o.data.Len() {
		o.cancel()
		return 0, safeerror.NewFault(fault.Invalid, "doctor tool output exceeds its bound")
	}
	return o.data.Write(p)
}

func run(ctx context.Context, options Options, executable string, args ...string) ([]byte, error) {
	return runBounded(ctx, options, maxOutputBytes, io.Discard, executable, args...)
}

// headWriter keeps the first bytes written to it and drops the rest, so a tool's
// diagnostic stays bounded.
type headWriter struct {
	data  []byte
	limit int
}

func (h *headWriter) Write(p []byte) (int, error) {
	if room := h.limit - len(h.data); room > 0 {
		h.data = append(h.data, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// runBounded bounds the tool's output and time. Its stderr goes to the given writer,
// which must bound what it keeps.
func runBounded(ctx context.Context, options Options, limit int, stderr io.Writer, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = options.Dir
	command.WaitDelay = time.Second
	command.Env = offlineEnvironment(os.Environ())
	stdout := &output{limit: limit, cancel: cancel}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		return nil, safeerror.Join("operation failed", err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return stdout.data.Bytes(), nil
}
func offlineEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+10)
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GOTOOLCHAIN", "GOWORK", "GOPROXY", "GOSUMDB", "GOFLAGS", "GOENV", "GOPRIVATE", "GONOPROXY", "GONOSUMDB", "GOVCS":
			continue
		}
		result = append(result, entry)
	}
	return append(result, "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=readonly", "GOENV=off", "GOPRIVATE=", "GONOPROXY=none", "GONOSUMDB=none", "GOVCS=*:off")
}
