// Package clidiag captures framework command diagnostics without granting
// application-provided cli.Usage text diagnostic ownership.
package clidiag

import (
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Usage accepts only diagnostic text produced at a framework construction site.
func Usage(message string) error {
	return safeerror.Owned(message, cli.Usage(message))
}
