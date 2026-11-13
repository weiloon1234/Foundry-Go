package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

type admissionLifetimeFile struct {
	phase         string
	entered       chan struct{}
	release       chan struct{}
	opens, closes atomic.Int64
}

func (*admissionLifetimeFile) Name() string              { return "source.txt" }
func (*admissionLifetimeFile) ClientContentType() string { return "text/plain" }
func (f *admissionLifetimeFile) Open(context.Context) (io.ReadSeekCloser, error) {
	f.opens.Add(1)
	if f.phase == "open" {
		close(f.entered)
		<-f.release
	}
	return &admissionLifetimeReader{Reader: bytes.NewReader([]byte("payload")), file: f}, nil
}

type admissionLifetimeReader struct {
	*bytes.Reader
	file *admissionLifetimeFile
}

func (r *admissionLifetimeReader) Close() error {
	r.file.closes.Add(1)
	if r.file.phase == "close" {
		close(r.file.entered)
		<-r.file.release
	}
	return nil
}

func TestPostgresFileWritesAdmitAndDrainOpenThroughClose(t *testing.T) {
	for _, method := range []string{"prepare", "replace", "add"} {
		for _, phase := range []string{"open", "close"} {
			t.Run(method+"/"+phase, func(t *testing.T) {
				binding := extensions.SlotBinding[extensiontest.Member, int64, Many[extensiontest.Member]]{
					Field: "Documents",
					Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
						return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
					},
					Get: func(extensiontest.Member) Many[extensiontest.Member] { return Many[extensiontest.Member]{} },
					Set: func(m extensiontest.Member, _ Many[extensiontest.Member]) extensiontest.Member { return m },
				}
				policy := largePolicy()
				policy.Cardinality, policy.MaxFiles = Multiple, 4
				slot := ManyOf(extensiontest.Members, "file-lifetime", policy, binding)
				f := openAttachments(t, slot.Registration())
				config := DefaultConfig()
				config.MaxActive, config.MaxLargeActive = 2, 1
				config.TempDirectory = t.TempDir()
				manager, err := New(Dependencies{Store: f.Store, Disks: f.registry}, config, slot.Registration())
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close(context.Background())
				slot = slot.From(manager)
				owner := extensiontest.Member{ID: 1}
				run := func(ctx context.Context, file FileSource) error {
					switch method {
					case "prepare":
						_, err := slot.PrepareFile(ctx, owner, file)
						return err
					case "replace":
						_, err := slot.ReplaceFile(ctx, owner, file)
						return err
					default:
						_, err := slot.AddFile(ctx, owner, file)
						return err
					}
				}
				source := &admissionLifetimeFile{phase: phase, entered: make(chan struct{}), release: make(chan struct{})}
				done := make(chan error, 1)
				var once sync.Once
				joined := false
				defer func() {
					once.Do(func() { close(source.release) })
					if !joined {
						<-done
					}
				}()
				go func() { done <- run(context.Background(), source) }()
				select {
				case <-source.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("source callback not reached")
				}
				rejected := &admissionLifetimeFile{}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
				err = run(ctx, rejected)
				cancel()
				if !errors.Is(err, fault.Overloaded) || rejected.opens.Load() != 0 || rejected.closes.Load() != 0 {
					t.Fatal("saturated operation opened a source", err, rejected.opens.Load(), rejected.closes.Load())
				}
				ctx, cancel = context.WithTimeout(t.Context(), 30*time.Millisecond)
				err = manager.Close(ctx)
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("shutdown did not wait for source callback", err)
				}
				select {
				case <-manager.Done():
					t.Fatal("shutdown drained before source callback exited")
				default:
				}
				once.Do(func() { close(source.release) })
				select {
				case err = <-done:
					joined = true
				case <-time.After(5 * time.Second):
					t.Fatal("source did not drain")
				}
				if errors.Is(err, fault.Overloaded) {
					t.Fatal("file operation acquired admission twice", err)
				}
				if source.opens.Load() != 1 || source.closes.Load() != 1 {
					t.Fatal("source lifetime was not owned exactly once")
				}
				if err := manager.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				select {
				case <-manager.Done():
				default:
					t.Fatal("shutdown failed to finish")
				}
			})
		}
	}
}
