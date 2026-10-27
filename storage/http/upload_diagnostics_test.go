package http_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestUploadBridgeDiagnosticsSurviveCallback(t *testing.T) {
	for _, mode := range []string{"absent", "size mismatch", "closed request", "canceled open", "conditional publication"} {
		t.Run(mode, func(t *testing.T) {
			disk, key, _ := testDisk(t)
			batch, err := upload.New(t.Context(), upload.Config{TempDirectory: t.TempDir(), MaxBytes: 1024, MaxFileBytes: 1024, MaxFiles: 1, MaxReaders: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := batch.Close(); err != nil {
					t.Error(err)
				}
			})
			file, err := batch.Capture(t.Context(), strings.NewReader("replacement"), "private-client-name.txt", "text/plain")
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			options := storage.PutOptions{}
			code := storage.Invalid
			want := "consumer: storage_invalid: put"
			var cause error
			switch mode {
			case "absent":
				file = foundryhttp.UploadedFile{}
			case "size mismatch":
				options.Size = value.Set(int64(999))
			case "closed request":
				if err := batch.Close(); err != nil {
					t.Fatal(err)
				}
				code, cause, want = storage.Unavailable, fs.ErrClosed, "consumer: storage_unavailable: put"
			case "canceled open":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				code, cause, want = storage.Unavailable, context.Canceled, "consumer: storage_unavailable: put"
			case "conditional publication":
				options.Condition = storage.IfAbsent()
				code, want = storage.PreconditionFailed, "consumer: storage put: storage_precondition_failed: put"
			}
			var original error
			err = callback.Isolated("consumer", func() error {
				_, original = storagehttp.StoreUpload(ctx, disk, key, file, options)
				return original
			})
			var detail *storage.Error
			if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, code) || !errors.As(err, &detail) || detail.Operation() != storage.PutOperation || detail.Outcome() != storage.Unchanged {
				t.Fatal("upload diagnostic lost identity, classification or outcome")
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatal("upload cause identity lost")
			}
			*detail = *storage.Failure(storage.Code("private mutation"), storage.Operation("private operation"), storage.Unknown, nil)
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || disk.Stats().Active != 0 {
				t.Fatal("upload diagnostic was mutable or retained disk capacity")
			}
			data, _, err := disk.ReadBytes(t.Context(), key, 64, storage.ReadOptions{})
			if err != nil || string(data) != "abcdefghij" {
				t.Fatal("rejected upload changed published bytes")
			}
		})
	}
}
