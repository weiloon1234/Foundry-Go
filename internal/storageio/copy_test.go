package storageio

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/weiloon1234/Foundry-Go/storage"
	"io"
	"math"
	"strings"
	"testing"
)

func TestCopyBoundsAndDigestWithEOFAndShortWriters(t *testing.T) {
	for _, maximum := range []int64{0, 3, 4, math.MaxInt64} {
		n, digest, err := Copy(t.Context(), io.Discard, strings.NewReader("abc"), maximum)
		if maximum < 3 {
			if !errors.Is(err, storage.LimitExceeded) {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || n != 3 || digest != storage.SHA256(sha256.Sum256([]byte("abc"))) {
			t.Fatal("copy result", n, err)
		}
	}
	if _, _, err := Copy(t.Context(), shortWriter{}, strings.NewReader("abc"), 3); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	for _, mode := range []int{-1, BufferBytes + 1, 0} {
		if _, _, err := Copy(t.Context(), io.Discard, badReader(mode), 32); err == nil {
			t.Fatal("invalid reader accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Copy(ctx, io.Discard, strings.NewReader("abc"), 3); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type badReader int

func (r badReader) Read([]byte) (int, error) { return int(r), nil }
