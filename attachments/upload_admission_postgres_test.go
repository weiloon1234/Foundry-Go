package attachments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

type admissionReaderFunc func([]byte) (int, error)

func (f admissionReaderFunc) Read(p []byte) (int, error) { return f(p) }

type admissionFile struct {
	sourceFile
	opens *atomic.Int64
}

func (f admissionFile) Open(ctx context.Context) (io.ReadSeekCloser, error) {
	f.opens.Add(1)
	return f.sourceFile.Open(ctx)
}

func TestPostgresLargeUploadSublimitPrecedesSourcesAndPreservesSmallWork(t *testing.T) {
	large := Define(extensiontest.Members, "large-admission", largePolicy())
	binding := extensions.SlotBinding[extensiontest.Member, int64, One[extensiontest.Member]]{
		Field: "Document",
		Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
			return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
		},
		Get: func(extensiontest.Member) One[extensiontest.Member] { return One[extensiontest.Member]{} },
		Set: func(m extensiontest.Member, _ One[extensiontest.Member]) extensiontest.Member { return m },
	}
	slot := OneOf(extensiontest.Members, "large-admission-slot", largePolicy(), binding)
	f := openAttachments(t, large.Registration(), slot.Registration())
	config := DefaultConfig()
	config.MaxActive, config.MaxLargeActive, config.TempDirectory = 2, 1, t.TempDir()
	manager, err := New(Dependencies{Store: f.Store, Disks: f.registry}, config, large.Registration(), slot.Registration(), testSingle.Registration())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	slot = slot.From(manager)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	defer func() { once.Do(func() { close(release) }); <-done }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := member(t, 1)
	go func() {
		_, err := large.Prepare(ctx, manager, owner, Upload{OriginalName: "held.txt", Source: admissionReaderFunc(func([]byte) (int, error) {
			close(entered)
			<-release // The callback, not cancellation, ends ownership.
			return 0, io.ErrUnexpectedEOF
		})})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("source did not enter")
	}
	cancel()
	var reads, opens, requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = io.WriteString(w, "remote") }))
	defer server.Close()
	client := remoteClient(t, server, true)
	upload := func() Upload {
		return Upload{OriginalName: "queued.txt", Source: admissionReaderFunc(func([]byte) (int, error) { reads.Add(1); return 0, io.EOF })}
	}
	for _, tc := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"prepare", func(ctx context.Context) error { _, err := large.Prepare(ctx, manager, owner, upload()); return err }},
		{"add", func(ctx context.Context) error { _, err := large.Add(ctx, manager, owner, upload()); return err }},
		{"replace", func(ctx context.Context) error { _, err := large.Replace(ctx, manager, owner, upload()); return err }},
		{"accepts", func(ctx context.Context) error {
			_, err := slot.Accepts(ctx, admissionFile{sourceFile{"queued.txt", "", []byte("small input under large policy")}, &opens})
			return err
		}},
		{"url", func(ctx context.Context) error {
			_, err := large.AddFromURL(ctx, manager, owner, RemoteSource{Client: client, URL: server.URL + "/remote.txt"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			if err := tc.run(ctx); !errors.Is(err, fault.Overloaded) {
				t.Fatal("large source admitted while sublimit occupied", err)
			}
		})
	}
	if reads.Load() != 0 || opens.Load() != 0 || requests.Load() != 0 {
		t.Fatal("sublimit rejection performed source work", reads.Load(), opens.Load(), requests.Load())
	}
	if _, err := testSingle.Add(t.Context(), manager, member(t, 2), uploadText("small")); err != nil {
		t.Fatal("large work consumed reserved small capacity", err)
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		done <- err
		if err == nil {
			t.Fatal("canceled source succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("source did not exit")
	}
	prepared, err := large.Prepare(t.Context(), manager, owner, Upload{OriginalName: "recovered.txt", Source: strings.NewReader("recovered")})
	if err != nil {
		t.Fatal("large capacity did not recover", err)
	}
	if err := large.Discard(t.Context(), manager, prepared); err != nil {
		t.Fatal(err)
	}
	assertSpoolEmpty(t, config.TempDirectory)
}

func TestLargeUploadSublimitRequiresReservedSmallCapacity(t *testing.T) {
	for _, limit := range []int{-1, 2, 3} {
		config := DefaultConfig()
		config.MaxActive, config.MaxLargeActive = 2, limit
		if !errors.Is(config.Validate(), fault.Invalid) {
			t.Fatal("invalid large sublimit accepted", limit)
		}
	}
}
