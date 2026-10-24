package translations

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/i18n"
)

type handleOwner struct{ ID int64 }

func TestHandleSaveBoundaryKeepsConcreteOwnerAndLocales(t *testing.T) {
	sentinel := errors.New("write failed")
	calls := 0
	slot := TextHandle[handleOwner, int64]{core: &textHandleCore{save: func(_ context.Context, owner unsafe.Pointer, input map[i18n.LocaleID]string) error {
		calls++
		if *(*handleOwner)(owner) != (handleOwner{ID: 7}) || len(input) != 1 || input["en"] != "text" {
			t.Fatal("typed arguments changed")
		}
		return sentinel
	}}}
	if err := slot.Save(t.Context(), handleOwner{ID: 7}, map[i18n.LocaleID]string{"en": "text"}); err != sentinel || calls != 1 {
		t.Fatal("write outcome changed", err, calls)
	}
}
