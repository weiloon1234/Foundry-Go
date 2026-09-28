package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type blockedUploadReader struct {
	started chan struct{}
	release chan struct{}
	closed  bool
}

func (r *blockedUploadReader) Read([]byte) (int, error) {
	close(r.started)
	<-r.release
	return 0, io.EOF
}
func (r *blockedUploadReader) Close() error { r.closed = true; return nil }
func TestPostgresAttachmentCancellationRetainsActualReaderLifetime(t *testing.T) {
	f := openAttachments(t)
	config := DefaultConfig()
	config.MaxActive = 1
	m, err := New(Dependencies{Store: f.Store, Disks: f.registry}, config, testSingle.Registration())
	if err != nil {
		t.Fatal(err)
	}
	reader := &blockedUploadReader{started: make(chan struct{}), release: make(chan struct{})}
	returned := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _, err := testSingle.Add(ctx, m, member(t, 1), Upload{Source: reader}); returned <- err }()
	<-reader.started
	defer func() {
		close(reader.release)
		if err := <-returned; !errors.Is(err, context.Canceled) {
			t.Error("reader cancellation", err)
		}
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if reader.closed {
			t.Error("borrowed reader closed")
		}
	}()
	cancel()
	select {
	case err := <-returned:
		returned <- err
		t.Fatal("canceled upload abandoned reader")
	default:
	}
	if _, err := testSingle.Add(t.Context(), m, member(t, 1), uploadText("next")); !errors.Is(err, fault.Conflict) {
		t.Fatal("active canceled upload released its capacity", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer closeCancel()
	if err := m.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown ignored borrowed reader", err)
	}
	select {
	case <-m.Done():
		t.Fatal("manager Done before reader return")
	default:
	}
}

func TestPostgresAttachmentCanceledAcknowledgementRequiresSettlement(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	old := attachmentOf(t, addText(t, f, testSingle, owner, "old"))
	started := make(chan storage.ObjectKey, 1)
	release := make(chan struct{})
	returned := make(chan error, 1)
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		object, err := f.backend.Backend.Put(ctx, key, source, options)
		if err != nil {
			return object, err
		}
		started <- key
		<-release
		return object, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_, err := testSingle.Replace(ctx, f.manager, owner, uploadText("cancelled candidate"))
		returned <- err
	}()
	key := <-started
	id, err := model.ParseID[Operation](key.String()[strings.LastIndex(key.String(), "/")+1:])
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(release)
			<-returned
		}
	}()
	if _, err := f.manager.Settle(t.Context(), id, WriterStoppedAndStorageSettled, nil); err == nil {
		t.Fatal("active writer settlement accepted")
	}
	cancel()
	close(release)
	released = true
	if err := <-returned; err == nil {
		t.Fatal("canceled storage acknowledgement published")
	}
	f.backend.setPut(nil)
	state, err := f.manager.Inspect(t.Context(), id)
	if err != nil || state.State != Uncertain {
		t.Fatal("canceled write lost recovery intent", err)
	}
	assertBody(t, f, testSingle, owner, old, "old")
	state, err = f.manager.Settle(t.Context(), id, WriterStoppedAndStorageSettled, nil)
	if err != nil || state.State != Cleaned {
		t.Fatal("settled canceled upload", err)
	}
}
