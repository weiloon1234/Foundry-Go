package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Counter hashes give reproducible, nonrepeating content at arbitrary offsets
// without retaining a large fixture or replaying a PRNG from the beginning.
type rangedContent struct{}

func (rangedContent) ReadAt(p []byte, offset int64) (int, error) {
	n := len(p)
	for len(p) > 0 {
		var counter [8]byte
		binary.LittleEndian.PutUint64(counter[:], uint64(offset/sha256.Size))
		block := sha256.Sum256(counter[:])
		copied := copy(p, block[offset%sha256.Size:])
		p = p[copied:]
		offset += int64(copied)
	}
	return n, nil
}

func TestRealR2RangedReads(t *testing.T) {
	cfg := realConfig(t, R2)
	size := int64(96 << 20)
	if raw := os.Getenv("FOUNDRY_TEST_R2_LARGE_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatal("FOUNDRY_TEST_R2_LARGE_BYTES must be an integer byte count")
		}
		size = parsed
	}
	if size <= cfg.PartBytes || size > cfg.MaxObjectBytes {
		t.Fatalf("FOUNDRY_TEST_R2_LARGE_BYTES must be > %d and <= %d", cfg.PartBytes, cfg.MaxObjectBytes)
	}
	disk, _ := providerFactory(t, cfg)
	runRangedReads(t, disk, size, cfg.PartBytes)
}

func TestR2RangedReadsWire(t *testing.T) {
	_, backend, _, peer := conditionalWire(t)
	// Use the shipping backend, including its unchanged conditional-write cap.
	disk, err := storage.NewDisk("ranged-wire", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	runRangedReads(t, disk, 2*backend.config.PartBytes+123, backend.config.PartBytes)
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.invalid || peer.next != 1 || len(peer.uploads) != 0 {
		t.Fatal("expected one valid, completed multipart upload")
	}
}

func runRangedReads(t *testing.T, disk *storage.Disk, size, partBytes int64) {
	t.Helper()
	key, err := storage.ParseKey("ranged/large.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Register exact-object cleanup before publication, including unknown outcomes.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := disk.Delete(ctx, key, storage.DeleteOptions{}); err != nil {
			t.Error("ranged object cleanup", err)
		}
	})
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(rangedContent{}, 0, size)); err != nil {
		t.Fatal(err)
	}
	var digest storage.SHA256
	copy(digest[:], hash.Sum(nil))
	stored, err := disk.Put(t.Context(), key, io.NewSectionReader(rangedContent{}, 0, size), storage.PutOptions{Size: value.Set(size), Checksum: value.Set(digest), ContentType: storage.Binary})
	if err != nil {
		t.Fatal("multipart write", err)
	}
	if stored.Object.Size != size || stored.Object.ETag == "" {
		t.Fatal("multipart metadata mismatch")
	}
	options := storage.ReadOptions{IfMatch: stored.Object.ETag, Version: stored.Object.Version}
	spans := []storage.ByteRange{
		{Offset: 0, Length: 1}, {Offset: size - 1, Length: 1},
		{Offset: size - 17, Length: 64}, {Offset: partBytes - 17, Length: 64},
		{Offset: size / 3, Length: size - size/3},
	}
	random := rand.New(rand.NewPCG(35, 1))
	for range 48 {
		spans = append(spans, storage.ByteRange{Offset: random.Int64N(size), Length: 1 + random.Int64N(256<<10)})
	}
	for i, span := range spans {
		t.Run(fmt.Sprintf("range-%02d", i), func(t *testing.T) {
			opts := options
			opts.Range = value.Set(span)
			body, info, err := disk.Open(t.Context(), key, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := body.Close(); err != nil {
					t.Error(err)
				}
			}()
			length := min(span.Length, size-span.Offset)
			if info.Offset != span.Offset || info.Length != length || info.Object.Size != size || info.Object.ETag != stored.Object.ETag || info.Object.Version != stored.Object.Version {
				t.Fatal("range metadata mismatch", info.Offset, info.Length, info.Object.Size)
			}
			verifyRangedContent(t, body, span.Offset, length)
			requireRangedEOF(t, body)
		})
	}
	for _, tc := range []struct {
		span storage.ByteRange
		code storage.Code
	}{
		{storage.ByteRange{Offset: 0, Length: 0}, storage.Invalid},
		{storage.ByteRange{Offset: -1, Length: 1}, storage.Invalid},
		{storage.ByteRange{Offset: 0, Length: -1}, storage.Invalid},
		{storage.ByteRange{Offset: math.MaxInt64, Length: 1}, storage.Invalid},
		{storage.ByteRange{Offset: size, Length: 1}, storage.RangeNotSatisfiable},
		{storage.ByteRange{Offset: size + 1, Length: 1}, storage.RangeNotSatisfiable},
	} {
		opts := options
		opts.Range = value.Set(tc.span)
		body, _, err := disk.Open(t.Context(), key, opts)
		if body != nil {
			_ = body.Close()
			t.Fatal("invalid range returned a body")
		}
		if !errors.Is(err, tc.code) {
			t.Fatalf("range %+v: expected %v, got %v", tc.span, tc.code, err)
		}
	}
	t.Run("attachment-seek-reader", func(t *testing.T) {
		// attachments.Collection.OpenSeek delegates to this exact Disk method with
		// the stored ETag and version after its attachment authorization checks.
		body, info, err := disk.OpenSeek(t.Context(), key, options)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := body.Close(); err != nil {
				t.Error(err)
			}
		}()
		if info.Size != size || info.ETag != stored.Object.ETag || info.Version != stored.Object.Version {
			t.Fatal("seek metadata mismatch")
		}
		position := int64(0)
		for _, step := range []struct {
			offset int64
			whence int
			length int64
		}{
			{0, io.SeekStart, 31}, {partBytes - 7, io.SeekStart, 64},
			{-41, io.SeekCurrent, 97}, {11, io.SeekCurrent, 13},
			{-23, io.SeekEnd, 23}, {0, io.SeekEnd, 0},
			{19, io.SeekEnd, 0}, {size / 2, io.SeekStart, size - size/2},
			{0, io.SeekStart, 71},
		} {
			expected := step.offset
			if step.whence == io.SeekCurrent {
				expected += position
			}
			if step.whence == io.SeekEnd {
				expected += size
			}
			got, err := body.Seek(step.offset, step.whence)
			if err != nil || got != expected {
				t.Fatalf("seek: got %d, want %d: %v", got, expected, err)
			}
			verifyRangedContent(t, body, expected, step.length)
			position = expected + step.length
			if position >= size {
				requireRangedEOF(t, body)
			}
			if got, err := body.Seek(0, io.SeekCurrent); err != nil || got != position {
				t.Fatal("read did not advance seek position", got, err)
			}
		}
		for _, step := range []struct {
			offset int64
			whence int
		}{{-1, io.SeekStart}, {0, 99}} {
			if _, err := body.Seek(step.offset, step.whence); !errors.Is(err, storage.Invalid) {
				t.Fatal("invalid seek accepted", err)
			}
		}
		verifyRangedContent(t, body, position, 37)
	})
}

func verifyRangedContent(t *testing.T, reader io.Reader, offset, length int64) {
	t.Helper()
	var actual, expected [32 << 10]byte
	for length > 0 {
		chunk := min(length, int64(len(actual)))
		n, err := io.ReadFull(reader, actual[:chunk])
		if err != nil || int64(n) != chunk {
			t.Fatalf("read at %d: got %d, want %d: %v", offset, n, chunk, err)
		}
		_, _ = (rangedContent{}).ReadAt(expected[:chunk], offset)
		if !bytes.Equal(actual[:chunk], expected[:chunk]) {
			t.Fatalf("content mismatch in chunk at %d", offset)
		}
		offset += chunk
		length -= chunk
	}
}

func requireRangedEOF(t *testing.T, reader io.Reader) {
	t.Helper()
	var extra [1]byte
	if n, err := reader.Read(extra[:]); n != 0 || err != io.EOF {
		t.Fatalf("expected EOF: n=%d err=%v", n, err)
	}
}
