package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type batchOwner struct {
	ID    int64
	Files Many[batchOwner]
}
type batchFile struct{ opened *int }

func (f batchFile) Name() string              { return "file.txt" }
func (f batchFile) ClientContentType() string { return "text/plain" }
func (f batchFile) Open(context.Context) (io.ReadSeekCloser, error) {
	*f.opened++
	return readSeekCloser{bytes.NewReader([]byte("file"))}, nil
}

func TestAddFilesToValidatesBeforeOpening(t *testing.T) {
	table := orm.Define("batch_owners", func(b orm.Builder[batchOwner], m *batchOwner) struct {
		orm.Table[batchOwner, int64]
		Files orm.Slot[batchOwner, Many[batchOwner]]
	} {
		return struct {
			orm.Table[batchOwner, int64]
			Files orm.Slot[batchOwner, Many[batchOwner]]
		}{orm.TableOf(b, &m.ID), orm.SlotOf(b, &m.Files)}
	})
	owner := extensions.DefineORMOwner("batch", orm.IdentityOf(table.Table, table.Key()), extensions.OwnerOptions{})
	state := defineMany(owner, "files", Policy{Disk: storage.DefineDisk("files"), MaxFiles: 1, Accepted: []storage.MediaType{"text/plain"}}, extensions.BindSlot(table.Key(), table.Files))
	opened := 0
	files := []batchFile{{&opened}, {&opened}}
	for _, slot := range []ManyHandle[batchOwner, int64]{{}, {core: wrapManyHandle(state)}} {
		if result, err := AddFilesTo(t.Context(), slot, batchOwner{ID: 1}, files); err == nil || result != nil || opened != 0 {
			t.Fatal("invalid or unbound slot opened a file", err, opened)
		}
	}
	// A registered manager identity suffices for preflight; an oversized batch
	// must fail before either a file or the store is touched.
	collection := state.collection
	manager := &Manager{collections: map[string]Registration{collection.registrationKey(): collection.Registration()}}
	state = state.from(manager)
	next, nextErr := AddFilesTo(t.Context(), ManyHandle[batchOwner, int64]{core: wrapManyHandle(state)}, batchOwner{ID: 1}, files)
	if !errors.Is(nextErr, fault.Invalid) || next != nil || opened != 0 {
		t.Fatal("batch preflight changed", nextErr, opened)
	}
}

func TestAddFilesToRetainsEveryAttemptAndStopsOnUnknown(t *testing.T) {
	sentinel := errors.New("publication acknowledgement lost")
	opened, attempted := 0, 0
	core := &attachmentHandleCore{
		checkFiles: func(count int) error {
			if count != 3 || opened != 0 {
				t.Fatal("preflight was late")
			}
			return nil
		},
		addFile: func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error {
			if (*batchOwner)(owner).ID != 7 {
				t.Fatal("owner changed")
			}
			reader, err := file.Open(ctx)
			if err != nil {
				return err
			}
			if err = reader.Close(); err != nil {
				return err
			}
			attempted++
			result := Result[batchOwner, int64]{Publication: Published}
			if attempted == 2 {
				result.Publication = PublicationUnknown
				result.PendingVariants = true
			}
			*(*Result[batchOwner, int64])(out) = result
			if attempted == 2 {
				return sentinel
			}
			return nil
		},
	}
	results, err := AddFilesTo(t.Context(), ManyHandle[batchOwner, int64]{core: core}, batchOwner{ID: 7}, []batchFile{{&opened}, {&opened}, {&opened}})
	if err != sentinel || opened != 2 || attempted != 2 || len(results) != 2 || results[0].Publication != Published || results[1].Publication != PublicationUnknown || !results[1].PendingVariants {
		t.Fatal("partial publication lost or retried", err, opened, attempted, len(results))
	}
}
