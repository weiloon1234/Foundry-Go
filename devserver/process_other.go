//go:build !unix

package devserver

import (
	"context"
	"os/exec"
)

func configureProcess(*exec.Cmd) func() {
	return func() {}
}

func notifyHangup(ctx context.Context) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}
