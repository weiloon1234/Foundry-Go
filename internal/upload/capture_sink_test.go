package upload

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"io"
	"io/fs"
	"runtime"
	"strings"
	"testing"
)

func TestCaptureSourcesShareDigestAndMedia(t *testing.T) {
	batch := testBatch(t, t.Context(), nil)
	content := "complete captured content"
	readerFile, err := batch.Capture(t.Context(), strings.NewReader(content), "a.txt", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	var escaped io.Writer
	writerFile, err := batch.CaptureFrom(t.Context(), "b.txt", func(w io.Writer) (string, error) {
		escaped = w
		_, err := io.WriteString(w, content)
		return "text/csv", err
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(content))
	for _, file := range []File{readerFile, writerFile} {
		if file.SHA256() != expected || file.Size() != int64(len(content)) || file.ContentType() != "text/plain; charset=utf-8" || file.ClientContentType() != "text/csv" {
			t.Fatal("capture metadata differs")
		}
	}
	if _, err := escaped.Write([]byte("late")); !errors.Is(err, fs.ErrClosed) {
		t.Fatal("escaped sink accepted bytes", err)
	}
}

func TestCaptureFromDoesNotFinalizeFailedProducers(t *testing.T) {
	for _, kind := range []string{"error", "panic", "goexit", "ignored-limit", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			batch := testBatch(t, ctx, func(c *Config) { c.MaxFileBytes = 4 })
			file, err := batch.CaptureFrom(ctx, "input", func(w io.Writer) (string, error) {
				_, _ = w.Write([]byte("part"))
				switch kind {
				case "error":
					return "", io.ErrUnexpectedEOF
				case "panic":
					panic("private producer detail")
				case "goexit":
					runtime.Goexit()
				case "ignored-limit":
					_, _ = w.Write([]byte("!"))
				case "cancel":
					cancel()
				}
				return "text/plain", nil
			})
			if err == nil || !file.IsZero() || len(batch.names) != 0 {
				t.Fatal("failed producer published a file or retained partial bytes")
			}
		})
	}
}

func TestCaptureFromExactLimitAndEmptyDigest(t *testing.T) {
	for _, content := range []string{"", "1234"} {
		batch := testBatch(t, t.Context(), func(c *Config) { c.MaxFileBytes = 4 })
		file, err := batch.CaptureFrom(t.Context(), "input", func(w io.Writer) (string, error) { _, err := io.WriteString(w, content); return "", err })
		if err != nil || file.SHA256() != sha256.Sum256([]byte(content)) {
			t.Fatal("EOF at the declared limit rejected", err)
		}
	}
}

func TestCapturePreservesNativeHTTPErrorShapes(t *testing.T) {
	batch := testBatch(t, t.Context(), func(c *Config) { c.MaxFileBytes = 2 })
	_, err := batch.Capture(t.Context(), strings.NewReader("too large"), "input", "")
	if _, ok := safeerror.UnwrapOwned(err).(*LimitError); !ok {
		t.Fatal("limit no longer directly classifiable")
	}
	batch = testBatch(t, t.Context(), nil)
	_, err = batch.Capture(t.Context(), readFunc(func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }), "input", "")
	if _, ok := safeerror.UnwrapOwned(err).(*ReadError); !ok {
		t.Fatal("read failure no longer directly classifiable")
	}
}
