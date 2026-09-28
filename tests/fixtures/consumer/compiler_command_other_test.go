//go:build !unix

package consumer_test

import (
	"os/exec"
	"time"
)

// Platforms without POSIX process groups retain CommandContext cancellation.
// Bound inherited output pipes even where descendant cleanup is unavailable.
func configureCompilerCommand(cmd *exec.Cmd) { cmd.WaitDelay = time.Second }
