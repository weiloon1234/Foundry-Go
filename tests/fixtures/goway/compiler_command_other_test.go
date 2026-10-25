//go:build !unix

package goway_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os/exec"
)

// Platforms without POSIX process groups retain CommandContext cancellation.
// Bound inherited output pipes even where descendant cleanup is unavailable.
func configureCompilerCommand(cmd *exec.Cmd) { oracle.ConfigureCommand(cmd) }
