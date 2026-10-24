package http

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Diagnostic-only causes remain available to callers through errors.Is/As,
// but cannot supply transport classifications that their owner discarded before.
func walkTransportErrors(err error, visit func(error) bool) bool {
	return errorgraph.WalkPruned(err, visit, safeerror.IsDiagnostic)
}

func transportErrorAs[T error](err error) (value T, found, complete bool) {
	complete = walkTransportErrors(err, func(current error) bool {
		value, found = errorgraph.AsShallow[T](current)
		return !found
	})
	return value, found, complete
}
