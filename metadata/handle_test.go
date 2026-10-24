package metadata

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

type handleOwner struct{ ID int64 }
type handleInput struct{ Label string }

func TestHandleSaveBoundaryKeepsConcreteOwnerAndValue(t *testing.T) {
	sentinel := errors.New("write failed")
	calls := 0
	slot := ValueHandle[handleOwner, int64, handleInput]{core: &valueHandleCore{save: func(_ context.Context, owner, input unsafe.Pointer) error {
		calls++
		if *(*handleOwner)(owner) != (handleOwner{ID: 7}) || *(*handleInput)(input) != (handleInput{Label: "value"}) {
			t.Fatal("typed arguments changed")
		}
		return sentinel
	}}}
	if err := slot.Save(t.Context(), handleOwner{ID: 7}, handleInput{Label: "value"}); err != sentinel || calls != 1 {
		t.Fatal("write outcome changed", err, calls)
	}
}
