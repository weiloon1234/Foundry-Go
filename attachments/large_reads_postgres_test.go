package attachments

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
)

// This is the guide's deferred download pattern. The handler supplies an already
// authorized owner; OpenSeek rechecks ready membership when the source opens.
func largeAttachmentDownload(collection Collection[extensiontest.Member, int64], manager *Manager, owner model.Reference[extensiontest.Member, int64], id ID[extensiontest.Member]) foundryhttp.Download {
	return foundryhttp.DownloadFrom(func(ctx context.Context) (foundryhttp.DownloadContent, error) {
		body, file, err := collection.OpenSeek(ctx, manager, owner, id)
		if err != nil {
			if errors.Is(err, database.NotFound) {
				return foundryhttp.DownloadContent{}, foundryhttp.NotFound.WithCause(err)
			}
			return foundryhttp.DownloadContent{}, storagehttp.ResponseError(err)
		}
		return foundryhttp.DownloadContent{
			Body: body, Name: file.Info().OriginalName,
			MediaType: foundryhttp.MediaType(file.Info().MediaType),
			Modified:  file.CreatedAt().UTC(),
			EntityTag: foundryhttp.EntityTag(`"` + file.ID().String() + `"`),
		}, nil
	})
}

func TestPostgresLargeAttachmentHTTPPinnedRanges(t *testing.T) {
	collection := Define(extensiontest.Members, "large-read", largePolicy())
	var metadata *seekMetadataBackend
	f := openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
		metadata = &seekMetadataBackend{Backend: inner}
		return metadata
	}, collection.Registration())
	const size = MaxBufferedBytes + 1
	result, err := collection.Add(t.Context(), f.manager, member(t, 1), Upload{Source: io.LimitReader(repeatedByte('x'), size)})
	if err != nil {
		t.Fatal(err)
	}
	file := attachmentOf(t, result)
	etag := `"` + file.ID().String() + `"`
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "attachments.download", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse(foundryhttp.MediaType(file.Info().MediaType)),
	)
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return largeAttachmentDownload(collection, f.manager, member(t, 1), file.ID()), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, span, condition, validator string
		status, length                           int
		contentRange                             string
	}{
		{name: "head", method: "HEAD", status: 200},
		{name: "range", method: "GET", span: "bytes=67108860-67108863", status: 206, length: 4, contentRange: "bytes 67108860-67108863/67108865"},
		{name: "suffix", method: "GET", span: "bytes=-3", status: 206, length: 3, contentRange: "bytes 67108862-67108864/67108865"},
		{name: "open-ended", method: "GET", span: "bytes=67108862-", status: 206, length: 3, contentRange: "bytes 67108862-67108864/67108865"},
		{name: "if-range", method: "GET", span: "bytes=0-1", condition: "If-Range", validator: etag, status: 206, length: 2, contentRange: "bytes 0-1/67108865"},
		{name: "not-modified", method: "GET", condition: "If-None-Match", validator: etag, status: 304},
		{name: "unsatisfied", method: "GET", span: "bytes=67108865-", status: 416, contentRange: "bytes */67108865"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/file", nil)
			request.Header.Set("Range", tc.span)
			if tc.condition != "" {
				request.Header.Set(tc.condition, tc.validator)
			}
			response := httptest.NewRecorder()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			router.ServeHTTP(response, request)
			runtime.ReadMemStats(&after)
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("small HTTP response allocated %d bytes", allocated)
			}
			if response.Code != tc.status || response.Header().Get("Content-Range") != tc.contentRange {
				t.Fatal("range response", response.Code, response.Header())
			}
			if tc.status != 416 && response.Body.String() != strings.Repeat("x", tc.length) {
				t.Fatal("range body changed")
			}
			if tc.status == 200 && response.Header().Get("Content-Length") != strconv.FormatInt(size, 10) {
				t.Fatal("head length changed")
			}
			if f.disk.Stats().Streams != 0 {
				t.Fatal("HTTP retained source")
			}
		})
	}
	// A stale If-Range sends the whole representation without retaining it in
	// the recorder. The response body is still streamed through the HTTP bridge.
	request := httptest.NewRequest("GET", "/file", nil)
	request.Header.Set("Range", "bytes=0-1")
	request.Header.Set("If-Range", `"stale"`)
	response := &discardDownloadResponse{header: make(stdhttp.Header)}
	router.ServeHTTP(response, request)
	if response.status != 200 || response.size != size || f.disk.Stats().Streams != 0 {
		t.Fatal("stale If-Range did not stream full representation")
	}
	if _, _, err := collection.OpenSeek(t.Context(), f.manager, member(t, 2), file.ID()); !errors.Is(err, database.NotFound) {
		t.Fatal("cross-owner read accepted", err)
	}

	for _, mode := range []int32{1, 2, 3} {
		metadata.invalid.Store(mode)
		if reader, _, err := collection.OpenSeek(t.Context(), f.manager, member(t, 1), file.ID()); !errors.Is(err, storage.IntegrityFailed) {
			if reader != nil {
				reader.Close()
			}
			t.Fatal("invalid initial snapshot accepted", mode, err)
		}
	}
	metadata.invalid.Store(0)
	ctx, cancel := context.WithCancel(t.Context())
	reader, _, err := collection.OpenSeek(ctx, f.manager, member(t, 1), file.ID())
	if err != nil {
		t.Fatal(err)
	}
	// The lookup callback has returned: its cancellation must not cancel reads.
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		t.Fatal("lookup canceled handle", err)
	}
	cancel()
	if _, err := reader.Read(first[:]); err == nil {
		t.Fatal("caller cancellation ignored")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	reader, _, err = collection.OpenSeek(t.Context(), f.manager, member(t, 1), file.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disk.Put(t.Context(), file.key, strings.NewReader("replacement"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	// Move away from the current position so the storage reader reopens.
	if _, err := reader.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(first[:]); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("seek mixed a replacement", err)
	}
	if reader, _, err := collection.OpenSeek(t.Context(), f.manager, member(t, 1), file.ID()); err == nil {
		reader.Close()
		t.Fatal("stale persisted pin accepted")
	}
}

type discardDownloadResponse struct {
	header stdhttp.Header
	status int
	size   int64
}

func (w *discardDownloadResponse) Header() stdhttp.Header { return w.header }
func (w *discardDownloadResponse) WriteHeader(status int) { w.status = status }
func (w *discardDownloadResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.size += int64(len(p))
	return len(p), nil
}

// A faulty adapter must not override the attachment's persisted snapshot even
// when Stat succeeds. Reopened range identity is separately checked by Disk.
type seekMetadataBackend struct {
	storage.Backend
	invalid atomic.Int32
}

func (b *seekMetadataBackend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	info, err := b.Backend.Stat(ctx, key, options)
	switch b.invalid.Load() {
	case 1:
		info.Size++
	case 2:
		info.ETag = `"different"`
	case 3:
		info.Size--
	}
	return info, err
}
