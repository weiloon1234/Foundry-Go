package http_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

func testDisk(t *testing.T) (*storage.Disk, storage.ObjectKey, storage.StoredObject) {
	t.Helper()
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("files", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	key, err := storage.ParseKey("reports/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := disk.PutBytes(t.Context(), key, []byte("abcdefghij"), storage.PutOptions{ContentType: "text/plain; charset=utf-8"})
	if err != nil {
		t.Fatal(err)
	}
	return disk, key, stored
}
func TestStorageDownloadUsesNativeRangesValidatorsAndCleanup(t *testing.T) {
	disk, key, stored := testDisk(t)
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/plain; charset=utf-8"))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return storagehttp.Download(disk, key, storage.ReadOptions{}).WithName("file.txt"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, header, value string
		status                int
		body                  string
	}{
		{"GET", "", "", 200, "abcdefghij"}, {"HEAD", "", "", 200, ""}, {"GET", "Range", "bytes=2-5", 206, "cdef"}, {"GET", "Range", "bytes=-3", 206, "hij"}, {"GET", "If-None-Match", string(stored.Object.ETag), 304, ""}, {"GET", "If-Match", `"other"`, 412, ""}, {"GET", "Range", "bytes=20-30", 416, ""},
	} {
		t.Run(tc.method+tc.header+tc.value, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/file", nil)
			if tc.header != "" {
				request.Header.Set(tc.header, tc.value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || tc.status < 400 && response.Body.String() != tc.body {
				t.Fatal(response.Code, response.Body.String())
			}
			if disk.Stats().Active != 0 {
				t.Fatal("HTTP response leaked object reader")
			}
		})
	}
}
func TestStorageStreamAndMissingObject(t *testing.T) {
	disk, key, _ := testDisk(t)
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.stream", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.StreamResponse("text/plain; charset=utf-8"))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Stream, error) {
		return storagehttp.Stream(disk, key, storage.ReadOptions{}).WithName("file.txt"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/file", nil))
	if response.Code != 200 || response.Body.String() != "abcdefghij" || disk.Stats().Active != 0 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := disk.Delete(t.Context(), key, storage.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/file", nil))
	if response.Code != 404 || disk.Stats().Active != 0 {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestStorageCapacityExhaustionIsServiceUnavailable(t *testing.T) {
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	// The object is written through an ordinary disk; the serving disk has one
	// stream slot and a short admission wait, independent of fsync latency.
	writer, err := storage.NewDisk("writer", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	config := storage.DefaultConfig()
	config.MaxStreams, config.Timeout = 1, 200*time.Millisecond
	disk, err := storage.NewDisk("files", backend, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = disk.Close(context.Background())
		_ = writer.Close(context.Background())
		_ = backend.Close()
	})
	key, _ := storage.ParseKey("reports/file.txt")
	if _, err := writer.PutBytes(t.Context(), key, []byte("abc"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	held, _, err := disk.Open(t.Context(), key, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.busy", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.StreamResponse("text/plain; charset=utf-8"))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Stream, error) {
		return storagehttp.Stream(disk, key, storage.ReadOptions{}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/file", nil))
	if response.Code != 503 || response.Header().Get("Retry-After") == "" {
		t.Fatal("capacity exhaustion was not a retryable 503", response.Code, response.Body.String())
	}
}
