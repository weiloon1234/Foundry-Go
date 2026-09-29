package callback_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestNestedOwnedFailureKeepsConcreteClassification(t *testing.T) {
	for _, mode := range []string{"panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			err := callback.Isolated("outer", func() error {
				return callback.Isolated("inner", func() error {
					if mode == "exit" {
						runtime.Goexit()
					}
					panic("private application panic")
				})
			})
			failure, ok := err.(*fault.Error)
			if !ok || failure.Code() != fault.Panicked || strings.Contains(err.Error(), "private") {
				t.Fatal("nested classification lost", err)
			}
		})
	}
}
func TestOrdinaryCallbackErrorKeepsContextAndIdentity(t *testing.T) {
	original := errors.New("ordinary")
	err := callback.Isolated("operation", func() error { return original })
	if !errors.Is(err, original) || err.Error() != "operation: ordinary" {
		t.Fatal(err)
	}
}

func panickingHelper() { panic("private payload") }

func TestPanicRetainsSourceFramesWithoutPayload(t *testing.T) {
	err := callback.Invoke("operation", func() error { panickingHelper(); return nil })
	failure, ok := err.(*fault.Error)
	if !ok || failure.Code() != fault.Panicked {
		t.Fatal("panic classification lost", err)
	}
	frames := failure.Frames()
	if len(frames) == 0 || !strings.HasSuffix(frames[0].Function, "panickingHelper") || frames[0].Line == 0 {
		t.Fatalf("panic frames missing the panicking function: %+v", frames)
	}
	for _, frame := range frames {
		if strings.Contains(frame.String(), "private payload") {
			t.Fatal("frame exposed panic payload")
		}
	}
	isolated := callback.Isolated("isolated", func() error { panickingHelper(); return nil })
	if contained, ok := isolated.(*fault.Error); !ok || len(contained.Frames()) == 0 {
		t.Fatal("isolated panic lost frames", isolated)
	}
}
