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
