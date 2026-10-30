//go:build !unix

package devserver

import (
	"context"
	"os/exec"
)

func configureProcess(*exec.Cmd) func(bool) {
	return func(bool) {}
}

func notifyHangup(ctx context.Context) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}
