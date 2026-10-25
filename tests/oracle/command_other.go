//go:build !unix

package oracle

import (
	"os/exec"
	"time"
)

// ConfigureCommand bounds inherited pipes on platforms without process groups.
func ConfigureCommand(cmd *exec.Cmd) { cmd.WaitDelay = time.Second }
