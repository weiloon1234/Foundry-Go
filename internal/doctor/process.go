package doctor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const maxOutputBytes = 64 << 10

// A named buffer prevents io.Copy from selecting an embedded ReaderFrom and
// bypassing Write's bound. Tool stderr is discarded, never replayed in reports.
type output struct {
	data   bytes.Buffer
	cancel context.CancelFunc
}

func (o *output) Write(p []byte) (int, error) {
	if len(p) > maxOutputBytes-o.data.Len() {
		o.cancel()
		return 0, fault.New(fault.Invalid, "doctor tool output exceeds its bound")
	}
	return o.data.Write(p)
}

func run(ctx context.Context, options Options, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = options.Dir
	command.WaitDelay = time.Second
	command.Env = offlineEnvironment(os.Environ())
	stdout := &output{cancel: cancel}
	command.Stdout = stdout
	command.Stderr = io.Discard
	err := command.Run()
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
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
