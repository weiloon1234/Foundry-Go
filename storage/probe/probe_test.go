package probe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/probe"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type backend struct {
	storage.Backend
	objects                    map[storage.ObjectKey][]byte
	keys                       []storage.ObjectKey
	putErr, readErr, deleteErr error
	mismatch                   bool
	deletes                    int
	cancelRead                 context.CancelFunc
	cleanupContextErr          error
	cleanupRemaining           time.Duration
}

func (*backend) Capabilities() storage.Capabilities { return storage.Capabilities{} }
func metadata(key storage.ObjectKey, size int) storage.ObjectInfo {
	modified, _ := temporal.NewDateTime(time.Unix(1700000000, 0))
	return storage.ObjectInfo{Key: key, Size: int64(size), ContentType: storage.Binary, Modified: modified, ETag: `"probe"`}
}
func (b *backend) Put(_ context.Context, key storage.ObjectKey, input io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
	b.keys = append(b.keys, key)
	data, err := io.ReadAll(input)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if b.putErr != nil {
		var failure *storage.Error
		if errors.As(b.putErr, &failure) && failure.Outcome() != storage.Unchanged {
			b.objects[key] = data
		}
		return storage.ObjectInfo{}, b.putErr
	}
	b.objects[key] = data
	return metadata(key, len(data)), nil
}
func (b *backend) Open(_ context.Context, key storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	if b.cancelRead != nil {
		b.cancelRead()
	}
	if b.readErr != nil {
		return nil, storage.ReadInfo{}, b.readErr
	}
	data := bytes.Clone(b.objects[key])
	if b.mismatch {
		data[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(data)), storage.ReadInfo{Object: metadata(key, len(data)), Length: int64(len(data))}, nil
}
func (b *backend) Delete(ctx context.Context, key storage.ObjectKey, _ storage.DeleteOptions) error {
	b.deletes++
	b.cleanupContextErr = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		b.cleanupRemaining = time.Until(deadline)
	}
	if b.deleteErr != nil {
		return b.deleteErr
	}
	delete(b.objects, key)
	return nil
}
func newDisk(t *testing.T, b *backend) *storage.Disk {
	t.Helper()
	b.objects = make(map[storage.ObjectKey][]byte)
	disk, err := storage.NewDisk("uploads", b, storage.DefaultConfig())
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
func TestDiskSuccessAndUniqueKeys(t *testing.T) {
	b := &backend{}
	disk := newDisk(t, b)
	for range 2 {
		if got := probe.Disk(t.Context(), disk); got != (probe.Result{Disk: "uploads", Outcome: "ok"}) {
			t.Fatal(got)
		}
		if len(b.objects) != 0 {
			t.Fatal("probe left an object")
		}
	}
	if len(b.keys) != 2 || b.keys[0] == b.keys[1] {
		t.Fatal("probe reused its key", b.keys)
	}
	for _, key := range b.keys {
		if !regexp.MustCompile(`^foundry-deploy-probe/[a-f0-9]{32}$`).MatchString(key.String()) {
			t.Fatal(key)
		}
	}
}
func TestDiskFailuresAndCleanup(t *testing.T) {
	private := errors.New("private endpoint credential material")
	forbidden := storage.Failure(storage.Forbidden, storage.PutOperation, storage.Unchanged, private)
	unknown := storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, private)
	applied := storage.Failure(storage.Unavailable, storage.PutOperation, storage.Applied, private)
	for _, test := range []struct {
		name        string
		backend     backend
		stage, code string
		deletes     int
		leftover    bool
	}{
		{"write", backend{putErr: forbidden}, "write", "storage_forbidden", 0, false},
		{"unknown-write", backend{putErr: unknown}, "write", "storage_unavailable", 1, false},
		{"applied-write", backend{putErr: applied}, "write", "storage_unavailable", 1, false},
		{"unknown-write-cleanup", backend{putErr: unknown, deleteErr: storage.Forbidden}, "write", "storage_unavailable", 1, true},
		{"read", backend{readErr: storage.Failure(storage.Forbidden, storage.OpenOperation, storage.NotApplicable, private)}, "read", "storage_forbidden", 1, false},
		{"mismatch", backend{mismatch: true}, "read", "storage_probe_mismatch", 1, false},
		{"read-cleanup", backend{readErr: storage.Forbidden, deleteErr: storage.Forbidden}, "read", "storage_forbidden", 1, true},
		{"mismatch-cleanup", backend{mismatch: true, deleteErr: storage.Forbidden}, "read", "storage_probe_mismatch", 1, true},
		{"delete", backend{deleteErr: storage.Failure(storage.Forbidden, storage.DeleteOperation, storage.Unchanged, private)}, "delete", "storage_forbidden", 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &test.backend
			got := probe.Disk(t.Context(), newDisk(t, b))
			if got.Disk != "uploads" || got.Outcome != "failed" || got.Stage != test.stage || got.Code != test.code {
				t.Fatal(got)
			}
			if b.deletes != test.deletes || (got.LeftoverKey != "") != test.leftover {
				t.Fatal("wrong cleanup", got, b.deletes)
			}
			if test.leftover && got.LeftoverKey != b.keys[0].String() {
				t.Fatal("lost cleanup key", got)
			}
			if !test.leftover && len(b.objects) != 0 {
				t.Fatal("probe left an object")
			}
			encoded, err := json.Marshal(got)
			if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "credential") {
				t.Fatal("unsafe result", string(encoded), err)
			}
		})
	}
}
func TestReadFailureCleansUpAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b := &backend{readErr: storage.Unavailable, cancelRead: cancel}
	got := probe.Disk(ctx, newDisk(t, b))
	if got.Outcome != "failed" || got.Stage != "read" || got.LeftoverKey != "" || len(b.objects) != 0 || b.deletes != 1 {
		t.Fatal("cancelled read did not clean up", got)
	}
	if b.cleanupContextErr != nil || b.cleanupRemaining <= 0 || b.cleanupRemaining > 10*time.Second {
		t.Fatal("cleanup context was cancelled or unbounded")
	}
}
