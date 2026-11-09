package closeerror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
)

func TestOnlyDisconnectedPreservesOtherFailures(t *testing.T) {
	if !OnlyDisconnected(errors.Join(net.ErrClosed, &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}), nil) {
		t.Fatal("joined disconnects were rejected")
	}
	for _, cause := range []error{
		context.DeadlineExceeded, context.Canceled, errors.New("private cleanup failure"),
		fault.Wrap(fault.Internal, "cleanup failed", io.EOF),
		&errorgraphtest.Error{Mode: "cycle"}, &errorgraphtest.Error{Mode: "panic"},
		&errorgraphtest.Error{Mode: "goexit"}, emptyCloseWrapper{},
	} {
		for _, combined := range []error{cause, errors.Join(net.ErrClosed, cause), errors.Join(cause, net.ErrClosed)} {
			if OnlyDisconnected(combined, nil) {
				t.Fatalf("close discarded a genuine or uninspectable failure: %T", cause)
			}
		}
	}
}

type emptyCloseWrapper struct{}

func (emptyCloseWrapper) Error() string   { panic("private error must not be formatted") }
func (emptyCloseWrapper) Unwrap() []error { return []error{nil} }

func TestOnlyDisconnectedVisitsStatefulUnwrapChild(t *testing.T) {
	for _, name := range []string{"single", "multiple"} {
		t.Run(name, func(t *testing.T) {
			child := errors.New("genuine cleanup failure")
			state := &changingCloseWrapper{child: child}
			var wrapper error = state
			if name == "multiple" {
				wrapper = changingCloseJoin{state}
			}
			if OnlyDisconnected(errors.Join(net.ErrClosed, wrapper), nil) {
				t.Fatal("stateful unwrap hid a genuine cleanup failure")
			}
			if state.calls != 1 {
				t.Fatalf("unwrap called %d times, want once", state.calls)
			}
		})
	}
}

type changingCloseWrapper struct {
	child error
	calls int
}

func (*changingCloseWrapper) Error() string { return "stateful close error" }
func (e *changingCloseWrapper) Unwrap() error {
	e.calls++
	if e.calls == 1 {
		return e.child
	}
	return nil
}

type changingCloseJoin struct{ state *changingCloseWrapper }

func (changingCloseJoin) Error() string     { return "stateful close join" }
func (e changingCloseJoin) Unwrap() []error { return []error{e.state.Unwrap()} }

func TestOnlyDisconnectedRequiresTransport(t *testing.T) {
	for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, syscall.EPIPE, syscall.ECONNRESET} {
		socket := &net.OpError{Op: "write", Net: "tcp", Err: fmt.Errorf("socket: %w", cause)}
		if !OnlyDisconnected(fmt.Errorf("close_notify: %w", socket), nil) {
			t.Fatalf("transport disconnect was rejected: %T", cause)
		}
		for _, err := range []error{
			cause, fmt.Errorf("encrypt: %w", cause),
			errors.Join(socket, cause), errors.Join(cause, socket),
			errors.Join(socket, fmt.Errorf("encrypt: %w", cause)),
		} {
			if OnlyDisconnected(err, nil) {
				t.Fatalf("disconnect without transport provenance was accepted: %T", cause)
			}
		}
	}
	if !OnlyDisconnected(&net.OpError{Op: "write", Err: errors.Join(io.EOF, syscall.EPIPE)}, nil) {
		t.Fatal("transport ancestry was lost across joined children")
	}
}
