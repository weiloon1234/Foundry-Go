package http_test

import (
	"context"
	"io"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
)

type cyclicDownloadError struct{ visits atomic.Int32 }

func (*cyclicDownloadError) Error() string { panic("private download error formatted") }
func (e *cyclicDownloadError) Unwrap() error {
	// Old implementations eventually return so the assertion can report failure.
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

type failingDownloadBackend struct {
	storage.Backend
	failure error
}

func (f failingDownloadBackend) Capabilities() storage.Capabilities {
	return storage.Capabilities{Ranges: true, ConditionalRead: true}
}
func (f failingDownloadBackend) Stat(context.Context, storage.ObjectKey, storage.ReadOptions) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, f.failure
}
func (f failingDownloadBackend) Open(context.Context, storage.ObjectKey, storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	return nil, storage.ReadInfo{}, f.failure
}

func TestCyclicStorageErrorsFinishHTTPDownloadAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "download"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			cycle := &cyclicDownloadError{}
			disk, err := storage.NewDisk("files", failingDownloadBackend{failure: cycle}, storage.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := disk.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			key, err := storage.ParseKey("report.txt")
			if err != nil {
				t.Fatal(err)
			}
			route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.failure", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file"))
			var registration foundryhttp.RouteRegistration
			if stream {
				endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.StreamResponse("text/plain"))
				registration = endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Stream, error) {
					return storagehttp.Stream(disk, key, storage.ReadOptions{}), nil
				})
			} else {
				endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/plain"))
				registration = endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
					return storagehttp.Download(disk, key, storage.ReadOptions{}), nil
				})
			}
			router, err := foundryhttp.NewRouter(registration)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/file", nil))
			if response.Code != 500 || disk.Stats().Active != 0 {
				t.Fatal("failed response did not finish cleanly", response.Code)
			}
			if cycle.visits.Load() == 0 || cycle.visits.Load() > 3*256 {
				t.Fatal("download error traversal was not bounded", cycle.visits.Load())
			}
		})
	}
}
