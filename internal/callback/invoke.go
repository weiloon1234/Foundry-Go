// Package callback isolates extension failures without formatting panic payloads.
package callback

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// maxPanicFrames bounds retained panic locations; the innermost frames matter.
const maxPanicFrames = 32

// Invoke catches panics and preserves ordinary callback error identity.
// runtime.Goexit still exits this goroutine; use Isolated when it must be caught.
// A contained panic keeps its source frames, never its recovered value.
func Invoke(operation string, fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fault.Panic(operation+" panicked", panicFrames())
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

// panicFrames runs inside Invoke's deferred recovery, where the panicking
// goroutine's stack is still intact. It records function/file/line only.
func panicFrames() []fault.Frame {
	var pcs [maxPanicFrames + 16]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	result := make([]fault.Frame, 0, 8)
	started := false
	for {
		frame, more := frames.Next()
		// Skip the runtime's panic machinery above the panicking frame.
		if !started && (strings.HasPrefix(frame.Function, "runtime.") || frame.Function == "") {
			if !more {
				break
			}
			continue
		}
		started = true
		result = append(result, fault.Frame{Function: frame.Function, File: frame.File, Line: frame.Line})
		if !more || len(result) == maxPanicFrames {
			break
		}
	}
	return result
}
