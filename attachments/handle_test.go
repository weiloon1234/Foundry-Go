package attachments

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
)

type handleOwner struct{ ID int64 }

func TestHandlePublicationBoundaryKeepsOwnerPreparedAndUnknownResult(t *testing.T) {
	sentinel := errors.New("publication acknowledgement lost")
	owner := handleOwner{ID: 7}
	prepared := Prepared[handleOwner, int64]{collection: "cover"}
	calls := 0
	core := &attachmentHandleCore{replaceIn: func(_ context.Context, tx *database.Tx, parent, upload, out unsafe.Pointer) error {
		calls++
		if *(*handleOwner)(parent) != owner || *(*Prepared[handleOwner, int64])(upload) != prepared {
			t.Fatal("typed arguments changed")
		}
		*(*Result[handleOwner, int64])(out) = Result[handleOwner, int64]{Publication: PublicationUnknown, PendingVariants: true}
		return sentinel
	}}
	slot := OneHandle[handleOwner, int64]{core: core}
	got, err := slot.ReplaceIn(t.Context(), nil, owner, prepared)
	if err != sentinel || got.Publication != PublicationUnknown || !got.PendingVariants || calls != 1 {
		t.Fatal("outcome changed or retried", got.Publication, err, calls)
	}
}
