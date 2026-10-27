package attachments

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

type privateAttachmentError struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateAttachmentError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private attachment diagnostic")
}
func (e *privateAttachmentError) Format(fmt.State, rune) { _ = e.Error() }

func TestPostgresAttachmentHookErrorsRetainIdentityWithoutFormatting(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("after=%t/exit=%t", after, exit), func(t *testing.T) {
				cause := &privateAttachmentError{exit: exit}
				// An application-created fault is not an owned diagnostic.
				returned := fault.Wrap(fault.Invalid, "private application fault", cause)
				hook := Hook[extensiontest.Member, int64]{}
				if after {
					hook.AfterStored = func(context.Context, *database.Tx, Attachment[extensiontest.Member, int64]) error { return returned }
				} else {
					hook.Before = func(context.Context, BeforeContext[extensiontest.Member, int64]) error { return returned }
				}
				collection := Define(extensiontest.Members, "private", testSingle.definition.policy, hook)
				f := openAttachments(t, collection.Registration())
				result, err := collection.Add(t.Context(), f.manager, member(t, 1), uploadText("candidate"))
				var retained *privateAttachmentError
				var retainedFault *fault.Error
				if !errors.Is(err, returned) || !errors.Is(err, cause) || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause || !errors.As(err, &retainedFault) || retainedFault != returned {
					t.Fatal("attachment hook lost its cause or classification")
				}
				if result.Publication != Unpublished || result.Attachment.IsSet() {
					t.Fatal("rejected hook published an attachment")
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.calls.Load() != 0 {
					t.Fatal("attachment hook error invoked or exposed application formatting")
				}
			})
		}
	}
}
