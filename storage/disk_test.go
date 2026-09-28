package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type backend struct {
	storage.Backend
	put  func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error)
	open func(context.Context, storage.ObjectKey, storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error)
	stat func(context.Context, storage.ObjectKey, storage.ReadOptions) (storage.ObjectInfo, error)
}

func (*backend) Capabilities() storage.Capabilities {
	return storage.Capabilities{Ranges: true, ConditionalRead: true}
}
func (b *backend) Put(c context.Context, k storage.ObjectKey, r io.Reader, o storage.PutOptions) (storage.ObjectInfo, error) {
	return b.put(c, k, r, o)
}
func (b *backend) Open(c context.Context, k storage.ObjectKey, o storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	return b.open(c, k, o)
}
func (b *backend) Stat(c context.Context, k storage.ObjectKey, o storage.ReadOptions) (storage.ObjectInfo, error) {
	return b.stat(c, k, o)
}
func testKey(t *testing.T) storage.ObjectKey {
	t.Helper()
	key, err := storage.ParseKey("object")
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func metadata(key storage.ObjectKey, size int64) storage.ObjectInfo {
	modified, _ := temporal.NewDateTime(time.Unix(1700000000, 0))
	return storage.ObjectInfo{Key: key, Size: size, ContentType: storage.Binary, Modified: modified, ETag: `"one"`}
}
func diskFor(t *testing.T, b storage.Backend, active int) *storage.Disk {
	t.Helper()
	cfg := storage.DefaultConfig()
	cfg.MaxActive = active
	disk, err := storage.NewDisk("test", b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := disk.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return disk
}
func TestPutRejectsUnconsumedSourceAndCallbackFailure(t *testing.T) {
	for _, mode := range []string{"early", "panic", "goexit", "wrong-size"} {
		t.Run(mode, func(t *testing.T) {
			b := &backend{put: func(_ context.Context, k storage.ObjectKey, r io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
				switch mode {
				case "panic":
					panic("private provider credential")
				case "goexit":
					runtime.Goexit()
				case "wrong-size":
					_, err := io.Copy(io.Discard, r)
					if err != nil {
						return storage.ObjectInfo{}, err
					}
				}
				return metadata(k, 1), nil
			}}
			disk := diskFor(t, b, 1)
			result, err := disk.Put(t.Context(), testKey(t), strings.NewReader("payload"), storage.PutOptions{})
			if err == nil || result != (storage.StoredObject{}) || strings.Contains(fmt.Sprintf("%+v", err), "private") {
				t.Fatal("invalid adapter succeeded or leaked details", err)
			}
			if disk.Stats().Active != 0 {
				t.Fatal("callback retained capacity")
			}
		})
	}
}
func TestPutPreservesLateCancellationOutcomeAndCleanupReference(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	disk := diskFor(t, &backend{put: func(_ context.Context, k storage.ObjectKey, r io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
		data, err := io.ReadAll(r)
		if err != nil {
			return storage.ObjectInfo{}, err
		}
		cancel()
		return metadata(k, int64(len(data))), nil
	}}, 1)
	_, err := disk.Put(ctx, testKey(t), strings.NewReader("done"), storage.PutOptions{})
	var failure *storage.Error
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.Outcome() != storage.Applied {
		t.Fatal("published result lost", err)
	}
	id := storage.NewCleanupID("private-upload-reference")
	disk2 := diskFor(t, &backend{put: func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, errors.New("private endpoint token")).WithCleanup(id)
	}}, 1)
	_, err = disk2.Put(t.Context(), testKey(t), strings.NewReader("pending"), storage.PutOptions{})
	if !errors.As(err, &failure) || failure.Outcome() != storage.Unknown {
		t.Fatal(err)
	}
	cleanup, ok := failure.Cleanup().Get()
	if !ok || cleanup.Value() != id.Value() {
		t.Fatal("cleanup reference discarded")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", err, cleanup), "private") {
		t.Fatal("provider diagnostic leaked")
	}
}

type body struct {
	io.Reader
	closes atomic.Int32
}

func (b *body) Close() error { b.closes.Add(1); return nil }
func TestReadersOwnCapacityAndCloseOnPartialOpenFailure(t *testing.T) {
	for _, mode := range []string{"valid", "error", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			raw := &body{Reader: strings.NewReader("abc")}
			adapter := &backend{open: func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
				info := storage.ReadInfo{Object: metadata(k, 3), Length: 3}
				if mode == "error" {
					return raw, info, storage.NotFound
				}
				if mode == "metadata" {
					info.Length = 4
				}
				return raw, info, nil
			}, stat: func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (storage.ObjectInfo, error) {
				return metadata(k, 3), nil
			}}
			disk := diskFor(t, adapter, 1)
			reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
			if mode != "valid" {
				if err == nil || reader != nil || raw.closes.Load() != 1 || disk.Stats().Active != 0 {
					t.Fatal("partial open leaked", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := disk.Stat(t.Context(), testKey(t), storage.ReadOptions{}); !errors.Is(err, storage.LimitExceeded) {
				t.Fatal("open reader did not reserve capacity", err)
			}
			if err := disk.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			if raw.closes.Load() != 1 || disk.Stats().Active != 0 {
				t.Fatal("shutdown leaked reader")
			}
			if err := reader.Close(); err != nil || raw.closes.Load() != 1 {
				t.Fatal("close not idempotent", err)
			}
		})
	}
}

type blockedBody struct {
	entered, stop   chan struct{}
	once            sync.Once
	ctx             context.Context
	canceledOnClose atomic.Bool
}

func (b *blockedBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.stop
	return 0, io.ErrClosedPipe
}
func (b *blockedBody) Close() error {
	b.once.Do(func() { b.canceledOnClose.Store(b.ctx.Err() != nil); close(b.stop) })
	return nil
}
func TestClosingReaderOrDiskCancelsBeforeInterruptingRead(t *testing.T) {
	for _, mode := range []string{"reader", "disk"} {
		t.Run(mode, func(t *testing.T) {
			raw := &blockedBody{entered: make(chan struct{}), stop: make(chan struct{})}
			disk := diskFor(t, &backend{open: func(ctx context.Context, k storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
				raw.ctx = ctx
				return raw, storage.ReadInfo{Object: metadata(k, 3), Length: 3}, nil
			}}, 1)
			reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := reader.Read(make([]byte, 1)); result <- err }()
			<-raw.entered
			if mode == "disk" {
				err = disk.Close(t.Context())
			} else {
				err = reader.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatal("active read lost cancellation", err)
			}
			if !raw.canceledOnClose.Load() {
				t.Fatal("backend body closed before its context was canceled")
			}
			if disk.Stats().Active != 0 {
				t.Fatal("read capacity leaked")
			}
		})
	}
}
func TestReadProtocolFailureRetainsIntegrityClassification(t *testing.T) {
	for _, text := range []string{"short", "oversized data"} {
		t.Run(text, func(t *testing.T) {
			disk := diskFor(t, &backend{open: func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
				return io.NopCloser(strings.NewReader(text)), storage.ReadInfo{Object: metadata(k, 8), Length: 8}, nil
			}}, 1)
			reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(reader)
			_ = reader.Close()
			if err == nil {
				t.Fatal("invalid length accepted")
			}
		})
	}
}

func TestRegistryRejectsDuplicatesAndOwnsItsIndex(t *testing.T) {
	disk := diskFor(t, &backend{}, 1)
	if _, err := storage.NewRegistry(disk, disk); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate disk accepted", err)
	}
	registry, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	names := registry.Disks()
	names[0] = "changed"
	found, err := storage.DefineDisk("test").Resolve(registry)
	if err != nil || found != disk || registry.Disks()[0] != "test" {
		t.Fatal("registry ownership changed", err)
	}
}
