package attachments_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/attachmenttest"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	attachmentstest "github.com/weiloon1234/Foundry-Go/testkit/attachments"
	storagetest "github.com/weiloon1234/Foundry-Go/testkit/storage"
)

func TestRemoveBackingObjectPreservesMetadataAndOtherFiles(t *testing.T) {
	f := extensiontest.Open(t, attachments.Migrations())
	declaration := storage.DefineDisk("attachments")
	disk := storagetest.Local(t, declaration.ID())
	disks, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	collection := attachments.Define(extensiontest.Members, "documents", attachments.Policy{Disk: declaration, Cardinality: attachments.Multiple, Accepted: []storage.MediaType{"text/plain"}})
	manager, err := attachments.New(attachments.Dependencies{Store: f.Store, Disks: disks}, attachments.DefaultConfig(), collection.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	owner := extensions.ORMIdentity(extensiontest.Members).Reference(1)
	add := func(text string) attachments.Attachment[extensiontest.Member, int64] {
		t.Helper()
		result, err := collection.Add(t.Context(), manager, owner, attachments.Upload{Source: strings.NewReader(text), OriginalName: "note.txt"})
		if err != nil {
			t.Fatal(err)
		}
		file, ok := result.Attachment.Get()
		if !ok {
			t.Fatal("attachment absent")
		}
		return file
	}
	file, other := add("first object"), add("second object")
	if err := manager.RemoveBackingObject(t.Context(), attachmenttest.Token{}, model.IDFromBytes[attachments.Operation](file.ID().Bytes())); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid token accepted")
	}
	attachmentstest.RemoveBackingObject(t, manager, file.File)
	loaded, err := collection.Find(t.Context(), manager, owner, file.ID())
	if err != nil || loaded.ID() != file.ID() || loaded.Info() != file.Info() {
		t.Fatal("attachment metadata changed")
	}
	if _, err := collection.ReadBytes(t.Context(), manager, owner, file.ID(), 1024); !errors.Is(err, storage.NotFound) {
		t.Fatal("missing object did not return storage.NotFound")
	}
	if body, err := collection.ReadBytes(t.Context(), manager, owner, other.ID(), 1024); err != nil || string(body) != "second object" {
		t.Fatal("another attachment was affected")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if fmt.Sprintf(format, loaded.File) != "model attachment file" {
			t.Fatal("file formatting exposed private state")
		}
	}
	state, err := manager.Inspect(t.Context(), model.IDFromBytes[attachments.Operation](file.ID().Bytes()))
	if err != nil || state.State != attachments.Ready {
		t.Fatal("attachment state changed")
	}
}
