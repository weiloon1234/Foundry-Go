package attachments

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recoveryProbe struct {
	storage.Backend
	omitChecksum bool
	bytes        atomic.Int64
	cancel       atomic.Pointer[context.CancelFunc]
}

func (b *recoveryProbe) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	info, err := b.Backend.Stat(ctx, key, options)
	if b.omitChecksum {
		info.Checksum = value.Optional[storage.SHA256]{}
	}
	return info, err
}
func (b *recoveryProbe) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	body, info, err := b.Backend.Open(ctx, key, options)
	if err != nil {
		return body, info, err
	}
	if b.omitChecksum {
		info.Object.Checksum = value.Optional[storage.SHA256]{}
	}
	return &recoveryProbeReader{ReadCloser: body, probe: b}, info, nil
}

type recoveryProbeReader struct {
	io.ReadCloser
	probe *recoveryProbe
}

func (r *recoveryProbeReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.probe.bytes.Add(int64(n))
	if cancel := r.probe.cancel.Swap(nil); cancel != nil {
		(*cancel)()
	}
	return n, err
}

func TestPostgresLargeRecoveryAfterPolicyChange(t *testing.T) {
	for _, path := range []string{"automatic-fallback", "explicit-with-metadata", "canceled-explicit"} {
		t.Run(path, func(t *testing.T) {
			collection := Define(extensiontest.Members, "large-recovery", largePolicy())
			var probe *recoveryProbe
			f := openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
				probe = &recoveryProbe{Backend: inner, omitChecksum: path == "automatic-fallback"}
				return probe
			}, collection.Registration())
			const size = MaxBufferedBytes + 1
			f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
				if _, err := f.backend.Backend.Put(ctx, key, source, options); err != nil {
					return storage.ObjectInfo{}, err
				}
				return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
			})
			result, err := collection.Add(t.Context(), f.manager, member(t, 1), Upload{Source: io.LimitReader(repeatedByte('x'), size)})
			if err == nil || result.Operation.IsZero() || journal(t, f, result.Operation).State != string(Uncertain) {
				t.Fatal("upload was not uncertain", err)
			}
			f.backend.setPut(nil)
			if err := f.manager.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			policy := largePolicy()
			policy.MaxBytes, policy.MaxStoredBytes = 1<<20, 1<<20
			changed := Define(extensiontest.Members, "large-recovery", policy)
			restarted, err := New(Dependencies{Store: f.Store, Disks: f.registry}, DefaultConfig(), changed.Registration())
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close(t.Context())
			row := journal(t, f, result.Operation)
			key, err := storage.ParseKey(row.ObjectKey)
			if err != nil {
				t.Fatal(err)
			}
			if path == "canceled-explicit" {
				ctx, cancel := context.WithCancel(t.Context())
				probe.cancel.Store(&cancel)
				_, err := restarted.Settle(ctx, result.Operation, WriterStoppedAndStorageSettled, nil)
				cancel()
				if err == nil || journal(t, f, result.Operation).State != string(Uncertain) {
					t.Fatal("partial verification retired intent", err)
				}
				if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
					t.Fatal("partial verification removed object", err)
				}
				if n := probe.bytes.Load(); n == 0 || n >= size {
					t.Fatal("cancellation did not interrupt verification", n)
				}
			}
			if path == "automatic-fallback" {
				if err := ageAttachmentIntents(t.Context(), f); err != nil {
					t.Fatal(err)
				}
			}
			beforeBytes := probe.bytes.Load()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			if path == "automatic-fallback" {
				if _, err := restarted.ReconcilePending(t.Context(), 10); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := restarted.Settle(t.Context(), result.Operation, WriterStoppedAndStorageSettled, nil); err != nil {
					t.Fatal(err)
				}
			}
			runtime.ReadMemStats(&after)
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("recovery allocated %d bytes", allocated)
			}
			if probe.bytes.Load()-beforeBytes != size || journal(t, f, result.Operation).State != string(Cleaned) {
				t.Fatal("recovery did not verify and reclaim historical original")
			}
			if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
				t.Fatal("recovery retained object", err)
			}
			if f.disk.Stats().Streams != 0 {
				t.Fatal("recovery retained stream admission")
			}
		})
	}
}
