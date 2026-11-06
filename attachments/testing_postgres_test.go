package attachments

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/attachmenttest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestRemoveBackingObjectRejectsInvalidInputsAndReplacement(t *testing.T) {
	f := openAttachments(t)
	file := attachmentOf(t, addText(t, f, testSingle, member(t, 1), "original"))
	id := model.IDFromBytes[Operation](file.ID().Bytes())
	for _, input := range []struct {
		manager *Manager
		token   attachmenttest.Token
		id      OperationID
	}{
		{f.manager, attachmenttest.Token{}, id},
		{f.manager, attachmenttest.Grant(), OperationID{}},
		{nil, attachmenttest.Grant(), id},
	} {
		if err := input.manager.RemoveBackingObject(t.Context(), input.token, input.id); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid removal accepted")
		}
	}
	assertBody(t, f, testSingle, member(t, 1), file, "original")
	condition, err := storage.IfMatch(file.etag)
	if err != nil {
		t.Fatal("prepare condition failed")
	}
	if _, err := f.disk.PutBytes(t.Context(), file.key, []byte("replacement"), storage.PutOptions{Condition: condition, ContentType: "text/plain"}); err != nil {
		t.Fatal("prepare replacement failed")
	}
	if err := f.manager.RemoveBackingObject(t.Context(), attachmenttest.Grant(), id); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("removal did not protect replacement")
	}
	if body, _, err := f.disk.ReadBytes(t.Context(), file.key, 1024, storage.ReadOptions{}); err != nil || string(body) != "replacement" {
		t.Fatal("replacement object changed")
	}
}
