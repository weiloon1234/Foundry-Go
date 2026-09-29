// Package faultwrap adds a feature's safe message to a failure without hiding
// the framework classification its cause already carries.
package faultwrap

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Wrap returns a fault.Error with message whose code is the first framework
// code found in err's bounded graph, so Overloaded, Invalid, Timeout, Closed,
// Conflict and Panicked causes keep their classification through feature
// boundaries. An unclassified deadline becomes Timeout; anything else, or an
// inspection that cannot complete, remains Internal. The cause stays available
// through errors.Is/errors.As and is never formatted. A nil err returns nil.
func Wrap(message string, err error) error {
	if err == nil {
		return nil
	}
	return fault.Wrap(Code(err), message, err)
}

// Code classifies err within errorgraph's bounds. Custom Unwrap/Is methods run
// in an isolated callback; a failing method conservatively yields Internal.
func Code(err error) fault.Code {
	code := fault.Internal
	if err == nil {
		return code
	}
	if direct, ok := err.(*fault.Error); ok && direct != nil {
		return direct.Code()
	}
	failed := callback.Isolated("classify framework fault", func() error {
		found, deadline := false, false
		complete := errorgraph.Walk(err, func(current error) bool {
			switch value := current.(type) {
			case *fault.Error:
				if value != nil {
					code, found = value.Code(), true
				}
			case fault.Code:
				code, found = value, true
			}
			deadline = deadline || errorgraph.Matches(current, context.DeadlineExceeded)
			return !found
		})
		if !complete {
			code = fault.Internal
		} else if !found && deadline {
			code = fault.Timeout
		}
		return nil
	})
	if failed != nil {
		return fault.Internal
	}
	return code
}
