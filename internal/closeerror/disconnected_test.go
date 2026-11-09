package closeerror

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
)

func TestOnlyDisconnectedPreservesOtherFailures(t *testing.T) {
	if !OnlyDisconnected(errors.Join(io.EOF, net.ErrClosed), nil) {
		t.Fatal("joined disconnects were rejected")
	}
	for _, cause := range []error{
		context.DeadlineExceeded, context.Canceled, errors.New("private cleanup failure"),
		fault.Wrap(fault.Internal, "cleanup failed", io.EOF),
		&errorgraphtest.Error{Mode: "cycle"}, &errorgraphtest.Error{Mode: "panic"},
		&errorgraphtest.Error{Mode: "goexit"}, emptyCloseWrapper{},
	} {
		for _, combined := range []error{cause, errors.Join(io.EOF, cause), errors.Join(cause, net.ErrClosed)} {
			if OnlyDisconnected(combined, nil) {
				t.Fatalf("close discarded a genuine or uninspectable failure: %T", cause)
			}
		}
	}
}

type emptyCloseWrapper struct{}

func (emptyCloseWrapper) Error() string   { panic("private error must not be formatted") }
func (emptyCloseWrapper) Unwrap() []error { return []error{nil} }
