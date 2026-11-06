// Package attachments supplies test-only attachment failure setup.
package attachments

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/internal/attachmenttest"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RemoveBackingObject deletes only file's pinned original object through manager,
// leaving attachment metadata and variants intact. Pass attachment.File for a
// collection result, or the File loaded into a model slot. Use isolated test data;
// this mutates real storage. Failure output never includes storage pins or causes.
func RemoveBackingObject[M any](t testing.TB, manager *attachments.Manager, file attachments.File[M]) {
	t.Helper()
	id := model.IDFromBytes[attachments.Operation](file.ID().Bytes())
	if err := manager.RemoveBackingObject(t.Context(), attachmenttest.Grant(), id); err != nil {
		t.Fatal("remove test attachment backing object failed")
	}
}
