package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func streamDisk(t *testing.T, b storage.Backend, configure func(*storage.Config)) *storage.Disk {
	t.Helper()
	cfg := storage.DefaultConfig()
	configure(&cfg)
	disk, err := storage.NewDisk("streams", b, cfg)
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
func readerBackend() *backend {
	return &backend{open: func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
		return io.NopCloser(strings.NewReader("abc")), storage.ReadInfo{Object: metadata(k, 3), Length: 3}, nil
	}, stat: func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (storage.ObjectInfo, error) {
		return metadata(k, 3), nil
	}}
}

func TestStreamsUseTheirOwnQueuedPool(t *testing.T) {
	disk := streamDisk(t, readerBackend(), func(c *storage.Config) { c.MaxActive, c.MaxStreams = 1, 1 })
	reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Stat(t.Context(), testKey(t), storage.ReadOptions{}); err != nil {
		t.Fatal("a slow download blocked metadata work", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _, err = disk.Open(ctx, testKey(t), storage.ReadOptions{})
	var failure *storage.Error
	if !errors.Is(err, fault.Overloaded) || !errors.As(err, &failure) || failure.Code() != storage.Unavailable || failure.Outcome() != storage.Unchanged {
		t.Fatal("exhausted stream capacity is not a retryable overload", err)
	}
	waited := make(chan error, 1)
	go func() {
		body, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
		if err == nil {
			err = body.Close()
		}
		waited <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatal("queued stream was not admitted after release", err)
	}
}

func TestIdleStreamIsCancelledButProgressKeepsItOpen(t *testing.T) {
	disk := streamDisk(t, readerBackend(), func(c *storage.Config) { c.Timeout, c.StreamIdleTimeout = time.Minute, 40*time.Millisecond })
	reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Progress restarts the idle deadline; the operation Timeout is not used.
	for range 3 {
		time.Sleep(20 * time.Millisecond)
		if _, err := reader.Read(make([]byte, 1)); err != nil {
			t.Fatal("progressing stream was cut off", err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for disk.Stats().Streams != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if disk.Stats().Streams != 0 {
		t.Fatal("idle stream kept its slot")
	}
	if _, err := reader.Read(make([]byte, 1)); err == nil {
		t.Fatal("idle stream remained readable")
	}
	_ = reader.Close()
}

func TestOperationsQueueForCapacity(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	b := &backend{put: func(_ context.Context, k storage.ObjectKey, r io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
		data, err := io.ReadAll(r)
		if err != nil {
			return storage.ObjectInfo{}, err
		}
		if string(data) == "first" {
			close(entered)
			<-release
		}
		return metadata(k, int64(len(data))), nil
	}}
	disk := diskFor(t, b, 1)
	first := make(chan error, 1)
	go func() {
		_, err := disk.PutBytes(t.Context(), testKey(t), []byte("first"), storage.PutOptions{})
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := disk.PutBytes(ctx, testKey(t), []byte("second"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
		t.Fatal("capacity wait did not end as overload", err)
	}
	second := make(chan error, 1)
	go func() {
		_, err := disk.PutBytes(t.Context(), testKey(t), []byte("second"), storage.PutOptions{})
		second <- err
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal("queued operation failed instead of waiting", err)
	}
}

func TestCapabilityCombinationsAreExplicit(t *testing.T) {
	aws := storage.Capabilities{ConditionalDelete: true, Versions: true}
	if err := aws.ValidateDelete(storage.DeleteOptions{IfMatch: `"one"`, Version: "v1"}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("unsupported conditional version delete accepted", err)
	}
	for _, options := range []storage.DeleteOptions{{Version: "v1"}, {IfMatch: `"one"`}, {}} {
		if err := aws.ValidateDelete(options); err != nil {
			t.Fatal("supported delete selector rejected", err)
		}
	}
	combined := aws
	combined.ConditionalVersionDelete = true
	if err := combined.ValidateDelete(storage.DeleteOptions{IfMatch: `"one"`, Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := (storage.Capabilities{}).ValidateList(storage.ListOptions{Delimited: true, Limit: 1}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("delimited listing accepted without capability", err)
	}
	if err := (storage.Capabilities{}).ValidatePut(storage.PutOptions{Metadata: storage.ObjectMetadata{CacheControl: "no-cache"}}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("object metadata silently ignored", err)
	}
	for _, metadata := range []storage.ObjectMetadata{{CacheControl: "bad\n"}, {Custom: map[string]string{"foundry-sha256": "x"}}, {Custom: map[string]string{"Upper": "x"}}, {StorageClass: "lower"}} {
		if err := metadata.Validate(); err == nil {
			t.Fatal("invalid object metadata accepted")
		}
	}
}

// listing is a Backend whose List returns a fixed page.
type listing struct {
	backend
	page storage.Page
}

func (l *listing) Capabilities() storage.Capabilities {
	return storage.Capabilities{DelimitedList: true}
}
func (l *listing) List(context.Context, storage.ListOptions) (storage.Page, error) {
	return l.page, nil
}

func TestListingAcceptsPartialMetadataAndCountsOversizedEntries(t *testing.T) {
	small, large := metadata(testKey(t), 2), metadata(key(t, "page/z"), 10)
	small.Key = key(t, "page/a")
	small.ContentType = ""
	directory, _ := storage.ParsePrefix("page/sub/")
	b := &listing{page: storage.Page{Objects: []storage.ObjectInfo{small, large}, Directories: []storage.Prefix{directory}}}
	disk := streamDisk(t, b, func(c *storage.Config) { c.MaxObjectBytes = 5 })
	prefix, _ := storage.ParsePrefix("page/")
	page, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 3, Delimited: true})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key.String() != "page/a" || page.Skipped != 1 || len(page.Directories) != 1 {
		t.Fatal("listing metadata contract changed", page, err)
	}
	b.page.Objects = []storage.ObjectInfo{metadata(key(t, "page/sub/deep"), 1)}
	if _, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 3, Delimited: true}); !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("nested object accepted in a one-level listing", err)
	}
	if _, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 3}); !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("directories accepted in a recursive listing", err)
	}
}
func key(t *testing.T, text string) storage.ObjectKey {
	t.Helper()
	parsed, err := storage.ParseKey(text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// copier implements ServerCopier on the shared test backend.
type copier struct {
	backend
	declined bool
	copies   int
}

func (c *copier) Copy(_ context.Context, _, target storage.ObjectKey, _ storage.CopyOptions, _ int64) (storage.ObjectInfo, error) {
	c.copies++
	if c.declined {
		return storage.ObjectInfo{}, storage.Failure(storage.Unsupported, storage.CopyOperation, storage.Unchanged, nil)
	}
	return metadata(target, 3), nil
}

func TestSameAdapterCopiesUseServerCopyAndFallBackWhenDeclined(t *testing.T) {
	for _, declined := range []bool{false, true} {
		streamed := 0
		c := &copier{declined: declined}
		c.open = func(_ context.Context, k storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
			streamed++
			return io.NopCloser(strings.NewReader("abc")), storage.ReadInfo{Object: metadata(k, 3), Length: 3}, nil
		}
		c.put = func(_ context.Context, k storage.ObjectKey, r io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
			data, err := io.ReadAll(r)
			return metadata(k, int64(len(data))), err
		}
		source := diskFor(t, c, 1)
		result, err := source.CopyTo(t.Context(), testKey(t), source, key(t, "copy"), storage.CopyOptions{})
		if err != nil || result.Object.Key.String() != "copy" || c.copies != 1 {
			t.Fatal("server copy was not attempted", err)
		}
		if declined != (streamed == 1) {
			t.Fatal("declined server copy did not stream exactly once", streamed)
		}
	}
}

func TestDeleteManyFallsBackToSingleDeletes(t *testing.T) {
	var deleted []string
	b := &deleter{deleted: &deleted}
	disk := diskFor(t, b, 1)
	result, err := disk.DeleteMany(t.Context(), []storage.ObjectKey{key(t, "a"), key(t, "b")})
	if err != nil || len(result.Deleted) != 2 || strings.Join(deleted, ",") != "a,b" {
		t.Fatal(result, err)
	}
	if _, err := disk.DeleteMany(t.Context(), []storage.ObjectKey{key(t, "a"), key(t, "a")}); !errors.Is(err, storage.Invalid) {
		t.Fatal("duplicate keys accepted", err)
	}
}

type deleter struct {
	backend
	deleted *[]string
}

func (d *deleter) Delete(_ context.Context, key storage.ObjectKey, _ storage.DeleteOptions) error {
	*d.deleted = append(*d.deleted, key.String())
	return nil
}
