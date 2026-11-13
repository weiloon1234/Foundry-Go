package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// The backend generates bytes on demand; neither the input nor output accumulates
// an object-sized buffer. All counters include actual reader work, not metadata.
type recoveryBackend struct {
	storage.Backend
	info                 storage.ObjectInfo
	open                 func(context.Context) io.Reader
	closeErr             error
	bytes, opens, closes atomic.Int64
}

func (*recoveryBackend) Capabilities() storage.Capabilities {
	return storage.Capabilities{Ranges: true, ConditionalRead: true, Versions: true}
}
func (b *recoveryBackend) Stat(context.Context, storage.ObjectKey, storage.ReadOptions) (storage.ObjectInfo, error) {
	return b.info, nil
}
func (b *recoveryBackend) Open(ctx context.Context, _ storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	if options.IfMatch != b.info.ETag || options.Version != b.info.Version {
		return nil, storage.ReadInfo{}, storage.Failure(storage.PreconditionFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	offset, length := int64(0), b.info.Size
	if span, ok := options.Range.Get(); ok {
		var err error
		offset, length, err = span.Resolve(b.info.Size)
		if err != nil {
			return nil, storage.ReadInfo{}, err
		}
	}
	var source io.Reader = io.LimitReader(repeatedByte('x'), length)
	if b.open != nil {
		source = b.open(ctx)
	}
	b.opens.Add(1)
	return &recoveryReader{Reader: source, backend: b}, storage.ReadInfo{Object: b.info, Offset: offset, Length: length}, nil
}

type recoveryReader struct {
	io.Reader
	backend *recoveryBackend
}

func (r *recoveryReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.backend.bytes.Add(int64(n))
	return n, err
}
func (r *recoveryReader) Close() error { r.backend.closes.Add(1); return r.backend.closeErr }

func recoveryDisk(t *testing.T, size int64) (*storage.Disk, *recoveryBackend, storage.SHA256) {
	t.Helper()
	key, err := storage.ParseKey("large-original")
	if err != nil {
		t.Fatal(err)
	}
	modified, err := temporal.NewDateTime(time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if n, err := io.CopyBuffer(hash, io.LimitReader(repeatedByte('x'), size), make([]byte, 32<<10)); err != nil || n != size {
		t.Fatal("expected digest source", n, err)
	}
	var digest storage.SHA256
	copy(digest[:], hash.Sum(nil))
	backend := &recoveryBackend{info: storage.ObjectInfo{Key: key, Size: size, ContentType: storage.Binary, Modified: modified, ETag: `"original"`, Version: "original-version"}}
	disk, err := storage.NewDisk("recovery", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil && (backend.closeErr == nil || !errors.Is(err, backend.closeErr)) {
			t.Error(err)
		}
	})
	return disk, backend, digest
}

func TestLargeRecoveryAndFullReadBoundedAllocations(t *testing.T) {
	for _, size := range []int64{128 << 20, 512 << 20} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			disk, backend, digest := recoveryDisk(t, size)
			for _, path := range []string{"fallback", "explicit-with-metadata", "full-read", "small-range"} {
				t.Run(path, func(t *testing.T) {
					backend.info.Checksum = value.Optional[storage.SHA256]{}
					if path == "explicit-with-metadata" {
						backend.info.Checksum = value.Set(digest)
					}
					beforeBytes := backend.bytes.Load()
					runtime.GC()
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					wantBytes := size
					switch path {
					case "fallback", "explicit-with-metadata":
						matched, err := new(Manager).matchesIntent(t.Context(), disk, backend.info.Key, store.File{Size: size, Digest: digest.String()}, backend.info, path == "fallback")
						if err != nil || !matched {
							t.Fatal("complete intent did not verify", matched, err)
						}
					case "full-read":
						body, _, err := disk.Open(t.Context(), backend.info.Key, storage.ReadOptions{IfMatch: backend.info.ETag, Version: backend.info.Version})
						if err != nil {
							t.Fatal(err)
						}
						reader := &verifiedReader{body: body, digest: sha256.New(), expected: digest, remaining: size}
						n, err := io.CopyBuffer(struct{ io.Writer }{io.Discard}, reader, make([]byte, 32<<10))
						closeErr := reader.Close()
						if err != nil || closeErr != nil || n != size {
							t.Fatal(n, err, closeErr)
						}
					case "small-range":
						reader, _, err := disk.OpenSeek(t.Context(), backend.info.Key, storage.ReadOptions{IfMatch: backend.info.ETag, Version: backend.info.Version})
						if err != nil {
							t.Fatal(err)
						}
						defer reader.Close()
						for _, offset := range []int64{size / 2, size - (32 << 10)} {
							if _, err := reader.Seek(offset, io.SeekStart); err != nil {
								t.Fatal(err)
							}
							if n, err := io.CopyN(io.Discard, reader, 32<<10); err != nil || n != 32<<10 {
								t.Fatal(n, err)
							}
						}
						if err := reader.Close(); err != nil {
							t.Fatal(err)
						}
						wantBytes = 64 << 10
					}
					runtime.ReadMemStats(&after)
					if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
						t.Fatalf("%s allocated %d bytes for %d-byte object", path, allocated, size)
					}
					if backend.bytes.Load()-beforeBytes != wantBytes || backend.opens.Load() != backend.closes.Load() || disk.Stats().Streams != 0 {
						t.Fatal("payload work or stream ownership mismatch")
					}
				})
			}
		})
	}
}

func TestRecoveryRejectsIncompleteEvidenceAndCloses(t *testing.T) {
	for _, mode := range []string{"short", "excess", "corrupt", "changed-size", "changed-etag", "changed-version", "cancel", "panic", "goexit", "close-error"} {
		t.Run(mode, func(t *testing.T) {
			disk, backend, digest := recoveryDisk(t, 1024)
			info := backend.info
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "short":
				backend.open = func(context.Context) io.Reader { return io.LimitReader(repeatedByte('x'), 1023) }
			case "excess":
				backend.open = func(context.Context) io.Reader { return io.LimitReader(repeatedByte('x'), 1025) }
			case "corrupt":
				backend.open = func(context.Context) io.Reader { return io.LimitReader(repeatedByte('y'), 1024) }
			case "changed-size":
				backend.info.Size++
			case "changed-etag":
				backend.info.ETag = `"other"`
			case "changed-version":
				backend.info.Version = "other-version"
			case "cancel":
				backend.open = func(context.Context) io.Reader {
					return recoveryReadFunc(func(p []byte) (int, error) { cancel(); return repeatedByte('x').Read(p) })
				}
			case "panic":
				backend.open = func(context.Context) io.Reader {
					return recoveryReadFunc(func([]byte) (int, error) { panic("private source") })
				}
			case "goexit":
				backend.open = func(context.Context) io.Reader {
					return recoveryReadFunc(func([]byte) (int, error) { runtime.Goexit(); return 0, nil })
				}
			case "close-error":
				backend.closeErr = errors.New("private close")
			}
			matched, err := new(Manager).matchesIntent(ctx, disk, info.Key, store.File{Size: info.Size, Digest: digest.String()}, info, false)
			if matched || err == nil {
				t.Fatal("incomplete evidence accepted", matched, err)
			}
			if backend.opens.Load() != backend.closes.Load() || disk.Stats().Streams != 0 {
				t.Fatal("verification leaked reader")
			}
		})
	}
}

type recoveryReadFunc func([]byte) (int, error)

func (fn recoveryReadFunc) Read(p []byte) (int, error) { return fn(p) }

func TestAutomaticRecoveryMetadataShortcutDoesNotRead(t *testing.T) {
	disk, backend, digest := recoveryDisk(t, 1024)
	backend.info.Checksum = value.Set(digest)
	matched, err := new(Manager).matchesIntent(t.Context(), disk, backend.info.Key, store.File{Size: 1024, Digest: digest.String()}, backend.info, true)
	if err != nil || !matched || backend.opens.Load() != 0 {
		t.Fatal("metadata shortcut changed", matched, err)
	}
}

func TestFullStreamingReadRequiresCompleteDigest(t *testing.T) {
	for _, mode := range []string{"short", "excess", "corrupt", "early-close"} {
		t.Run(mode, func(t *testing.T) {
			disk, backend, digest := recoveryDisk(t, 1024)
			size, content := int64(1024), repeatedByte('x')
			switch mode {
			case "short":
				size--
			case "excess":
				size++
			case "corrupt":
				content = repeatedByte('y')
			}
			backend.open = func(context.Context) io.Reader { return io.LimitReader(content, size) }
			body, _, err := disk.Open(t.Context(), backend.info.Key, storage.ReadOptions{IfMatch: backend.info.ETag, Version: backend.info.Version})
			if err != nil {
				t.Fatal(err)
			}
			reader := &verifiedReader{body: body, digest: sha256.New(), expected: digest, remaining: 1024}
			if mode == "early-close" {
				if _, err := io.CopyN(io.Discard, reader, 1); err != nil {
					t.Fatal(err)
				}
			} else if _, err := io.Copy(io.Discard, reader); err == nil {
				t.Fatal("incomplete full read verified")
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if disk.Stats().Streams != 0 || mode == "early-close" && backend.bytes.Load() != 1 {
				t.Fatal("close drained or retained source")
			}
		})
	}
}
