package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type repeatedByte byte

func (b repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func largePolicy() Policy {
	return (Policy{Disk: testDisk, Cardinality: Single, AnyMedia: true, MaxBytes: 512 << 20}).normalized()
}
func assertSpoolEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("preparation retained temporary files", err)
	}
}

func TestLargePoliciesRetainImageBounds(t *testing.T) {
	for _, size := range []int64{MaxBufferedBytes + 1, 512 << 20, math.MaxInt64} {
		policy := largePolicy()
		policy.MaxBytes, policy.MaxStoredBytes = size, size
		if err := policy.Validate(); err != nil {
			t.Fatal("opaque ceiling rejected", err)
		}
		policy.Image = value.Set(imaging.NewPlan())
		if err := policy.Validate(); err == nil {
			t.Fatal("oversized image policy accepted")
		}
		policy.Image = value.Optional[imaging.Plan]{}
		policy.AnyMedia = false
		policy.Accepted = []storage.MediaType{"image/png"}
		policy.Variants = []Variant{testThumbnail}
		if err := policy.Validate(); err == nil {
			t.Fatal("oversized variant policy accepted")
		}
	}
	if MaxUploadBytes != 64<<20 {
		t.Fatal("legacy limit changed")
	}
}

func TestLargePreparationOwnsSpoolAndDigestWithBoundedAllocation(t *testing.T) {
	directory := t.TempDir()
	m := &Manager{config: Config{TempDirectory: directory}}
	// These serial cases measure preparation only; the generator retains no payload.
	// The near-ceiling case permits one exact buffer for complete-content detection.
	for _, size := range []int64{MaxBufferedBytes - 1, MaxBufferedBytes + 1, 500 << 20} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		result, err := prepare(t.Context(), m, largePolicy(), Upload{Source: io.LimitReader(repeatedByte('x'), size), ContentType: "application/json"})
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result.body.(capturedBody); !ok {
			t.Fatal("large policy retained input in memory")
		}
		budget := uint64(8 << 20)
		if size <= MaxBufferedBytes {
			budget += uint64(size)
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
			t.Fatalf("preparation allocated %d bytes for %d bytes (budget %d)", allocated, size, budget)
		}
		if result.info.Size != size || result.info.MediaType != "text/plain" {
			t.Fatal("large media hint trusted")
		}
		reader, err := result.body.open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.New()
		n, err := io.CopyBuffer(digest, reader, make([]byte, 32<<10))
		if closeErr := reader.Close(); err != nil || closeErr != nil || n != size {
			t.Fatal("spool read", err, closeErr)
		}
		var expected storage.SHA256
		copy(expected[:], digest.Sum(nil))
		if expected != result.digest {
			t.Fatal("capture digest mismatch")
		}
		if err := result.body.close(); err != nil {
			t.Fatal(err)
		}
		if _, err := result.body.open(t.Context()); err == nil {
			t.Fatal("spool survived owner cleanup")
		}
		assertSpoolEmpty(t, directory)
	}
}

func TestLargePreparationLimitsAndFailureCleanup(t *testing.T) {
	directory := t.TempDir()
	m := &Manager{config: Config{TempDirectory: directory}}
	policy := largePolicy()
	policy.MaxStoredBytes = 3
	result, err := prepare(t.Context(), m, policy, uploadText("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := result.body.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(t.Context(), m, policy, uploadText("abcd")); !errors.Is(err, storage.LimitExceeded) {
		t.Fatal("effective stored ceiling not enforced", err)
	}
	assertSpoolEmpty(t, directory)
	for _, kind := range []string{"error", "panic", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := prepareCaptured(ctx, m, largePolicy(), Upload{}, true, func(batch *upload.Batch) (upload.File, error) {
				return batch.CaptureFrom(ctx, "input", func(w io.Writer) (string, error) {
					_, _ = io.WriteString(w, "partial")
					switch kind {
					case "panic":
						panic("private input")
					case "cancel":
						cancel()
						return "", nil
					default:
						return "", io.ErrUnexpectedEOF
					}
				})
			})
			if err == nil {
				t.Fatal("failed input accepted")
			}
			assertSpoolEmpty(t, directory)
		})
	}
	// Neither a huge declared ceiling nor a small body overflows size accounting.
	policy.MaxBytes, policy.MaxStoredBytes = math.MaxInt64, math.MaxInt64
	result, err = prepare(t.Context(), m, policy, uploadText("abc"))
	if err != nil {
		t.Fatal(err)
	}
	_ = result.body.close()
	assertSpoolEmpty(t, directory)
}

func TestHistoricalOriginalVariantGuardPrecedesStorage(t *testing.T) {
	for _, limit := range []int64{1024, 128 << 20} {
		config := imaging.DefaultConfig()
		config.Limits.InputBytes = limit
		engine, err := imaging.New(config)
		if err != nil {
			t.Fatal(err)
		}
		m := &Manager{image: engine} // No disk registry: touching storage before the guard fails.
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err = m.readOriginal(t.Context(), store.File{Size: min(limit, MaxBufferedBytes) + 1})
		runtime.ReadMemStats(&after)
		if !errors.Is(err, storage.LimitExceeded) {
			t.Fatal("historical original bypassed guard", err)
		}
		if after.TotalAlloc-before.TotalAlloc > 1<<20 {
			t.Fatal("variant rejection allocated content")
		}
		if err := engine.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLargeCapturedClassificationAndSmallSpecialization(t *testing.T) {
	m := &Manager{config: Config{TempDirectory: t.TempDir()}}
	for _, test := range []struct {
		prefix, hint, want string
		size               int64
	}{
		{"{}", "application/json", "application/json", 2},
		{"{", "application/json", "text/plain", MaxBufferedBytes + 1},
		{"PK\x03\x04", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip", MaxBufferedBytes + 1},
		{"\x00\x00\x00\x10ftypqt  \x00\x00\x00\x00", "application/octet-stream", "video/quicktime", MaxBufferedBytes + 1},
	} {
		source := io.MultiReader(strings.NewReader(test.prefix), io.LimitReader(repeatedByte(' '), test.size-int64(len(test.prefix))))
		result, err := prepare(t.Context(), m, largePolicy(), Upload{Source: source, ContentType: storage.MediaType(test.hint)})
		if err != nil {
			t.Fatal(err)
		}
		if string(result.info.MediaType) != test.want {
			t.Fatal("classification mismatch", result.info.MediaType, test.want)
		}
		_ = result.body.close()
	}
	assertSpoolEmpty(t, m.config.TempDirectory)
}

func TestSpoolClosesWhenStorageHookPanics(t *testing.T) {
	m := &Manager{config: Config{TempDirectory: t.TempDir()}}
	collection := Define(extensiontest.Members, "hook-cleanup", largePolicy(), Hook[extensiontest.Member, int64]{Before: func(context.Context, BeforeContext[extensiontest.Member, int64]) error { panic("private callback") }})
	result, err := prepare(t.Context(), m, largePolicy(), uploadText("sample"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = collection.storePrepared(t.Context(), m, member(t, 1), "", result, false)
	if err == nil {
		t.Fatal("panic not contained")
	}
	assertSpoolEmpty(t, m.config.TempDirectory)
}
