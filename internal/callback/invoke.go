// Package callback isolates extension failures without formatting panic payloads.
package callback

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Invoke catches panics and preserves ordinary callback error identity.
// runtime.Goexit still exits this goroutine; use Isolated when it must be caught.
func Invoke(operation string, fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fault.New(fault.Panicked, operation+" panicked")
		}
	}()
	if err = fn(); err != nil {
		// An inner owned callback already contained this failure. Keep its
		// concrete classification so an outer boundary need not inspect
		// arbitrary application error methods to discover the panic/Goexit.
		if failure, ok := err.(*fault.Error); ok && failure.Code() == fault.Panicked {
			return failure
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

// Isolated runs a callback in an owned goroutine and waits for its actual exit.
// It catches Goexit as well as panic. It never abandons an uncooperative callback
// or claims its resources have been released when it is still running.
func Isolated(operation string, fn func() error) error {
	result := make(chan error, 1)
	go func() {
		var err error = fault.New(fault.Panicked, operation+" exited without returning")
		defer func() { result <- err }()
		err = Invoke(operation, fn)
	}()
	return <-result
}
