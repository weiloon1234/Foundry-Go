package http

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

type faultyAssetFS struct {
	fstest.MapFS
	mode   string
	closes atomic.Int32
}

func (f *faultyAssetFS) Open(name string) (fs.File, error) {
	file, err := f.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	wrapped := &faultyAssetFile{File: file, owner: f}
	if f.mode == "open-error" {
		return wrapped, fs.ErrPermission
	}
	if f.mode == "no-seek" {
		return nonSeekingAssetFile{File: wrapped}, nil
	}
	return wrapped, nil
}

type nonSeekingAssetFile struct{ fs.File }
type faultyAssetFile struct {
	fs.File
	owner *faultyAssetFS
}

func (f *faultyAssetFile) Seek(offset int64, whence int) (int64, error) {
	return f.File.(io.Seeker).Seek(offset, whence)
}
func (f *faultyAssetFile) Stat() (fs.FileInfo, error) {
	if f.owner.mode == "stat-error" {
		return nil, errors.New("private-stat-detail")
	}
	info, err := f.File.Stat()
	if err == nil && f.owner.mode == "info-panic" {
		return faultyAssetInfo{FileInfo: info}, nil
	}
	return info, err
}
func (f *faultyAssetFile) Close() error {
	f.owner.closes.Add(1)
	switch f.owner.mode {
	case "close-panic":
		panic("private-close-detail")
	case "close-goexit":
		runtime.Goexit()
	case "close-error":
		return errors.New("private-close-detail")
	}
	return f.File.Close()
}

type faultyAssetInfo struct{ fs.FileInfo }

func (faultyAssetInfo) Name() string { panic("private-info-detail") }

func TestAssetSourcesReleaseBodiesOnEveryFailedOpenPath(t *testing.T) {
	for _, mode := range []string{"open-error", "no-seek", "stat-error", "info-panic", "close-error", "close-panic", "close-goexit"} {
		t.Run(mode, func(t *testing.T) {
			source := &faultyAssetFS{MapFS: assetFixture(), mode: mode}
			assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
			router, err := NewRouter(assets.Mount("assets", "/assets").Register())
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/assets/site.css", nil))
			status := 500
			if mode == "open-error" {
				status = 403
			}
			if strings.HasPrefix(mode, "close-") {
				status = 200
			}
			if recorder.Code != status || source.closes.Load() != 1 || strings.Contains(recorder.Body.String(), "private") {
				t.Fatal("resource ownership", mode, recorder.Code, source.closes.Load(), recorder.Body.String())
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := assets.Close(ctx); err != nil {
				t.Fatal("failed body retained lease", err)
			}
		})
	}
}

type blockingAssetFS struct {
	fstest.MapFS
	entered, release chan struct{}
	once             sync.Once
}

func (f *blockingAssetFS) Stat(name string) (fs.FileInfo, error) {
	f.once.Do(func() { close(f.entered); <-f.release })
	return f.MapFS.Stat(name)
}
func TestAssetShutdownRetainsCanceledStatUntilItsCallbackReturns(t *testing.T) {
	source := &blockingAssetFS{MapFS: assetFixture(), entered: make(chan struct{}), release: make(chan struct{})}
	var released sync.Once
	release := func() { released.Do(func() { close(source.release) }) }
	defer release()
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := assets.stat(ctx, "site.css"); done <- err }()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("stat did not enter")
	}
	cancel()
	if err := assets.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("close abandoned active callback", err)
	}
	select {
	case <-done:
		t.Fatal("stat returned before its callback")
	default:
	}
	select {
	case <-assets.done:
		t.Fatal("source closed during stat")
	default:
	}
	release()
	select {
	case err := <-done:
		if !errors.Is(err, RequestTimeout) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stat did not finish")
	}
	if err := assets.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAssetsRejectOversizedRepresentationsBeforeCommit(t *testing.T) {
	config := DefaultAssetsConfig(FilesystemAssets(assetFixture()))
	config.Limits.Bytes = 3
	assets := assetsForTest(t, config)
	router, err := NewRouter(assets.Mount("assets", "/assets").Register())
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/assets/site.css", nil))
	if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "abcdefghij") {
		t.Fatal("representation limit ignored", recorder.Code)
	}
	if err := assets.Close(t.Context()); err != nil {
		t.Fatal("oversized file retained its lease", err)
	}
}

type errorAssetFS struct {
	*faultyAssetFS
	failure error
	site    string
}

func (f *errorAssetFS) Stat(name string) (fs.FileInfo, error) {
	if f.site != "open" && f.failure != nil {
		return nil, f.failure
	}
	return f.MapFS.Stat(name)
}
func (f *errorAssetFS) Open(name string) (fs.File, error) {
	file, err := f.faultyAssetFS.Open(name)
	if err == nil && f.site == "open" && f.failure != nil {
		return file, f.failure
	}
	return file, err
}

type assetInspectionError struct{ fail func() }

func (*assetInspectionError) Error() string   { panic("private filesystem error must not be formatted") }
func (e *assetInspectionError) Is(error) bool { e.fail(); return false }

func TestAssetErrorInspectionReleasesFilesAndAllowsRecovery(t *testing.T) {
	for _, site := range []string{"stat", "open", "spa"} {
		for _, mode := range []string{"cycle", "panic", "goexit"} {
			t.Run(site+"-"+mode, func(t *testing.T) {
				cycle := new(cyclicHTTPError)
				var failure error = cycle
				if mode != "cycle" {
					failure = &assetInspectionError{fail: func() {
						if mode == "goexit" {
							runtime.Goexit()
						}
						panic("private filesystem classification")
					}}
				}
				source := &errorAssetFS{faultyAssetFS: &faultyAssetFS{MapFS: assetFixture()}, failure: failure, site: site}
				assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
				router, err := NewRouter(assets.Mount("assets", "/assets").Register())
				if err != nil {
					t.Fatal(err)
				}
				target, expected := "/assets/site.css", "abcdefghij"
				if site == "spa" {
					router, err = router.WithSPA("spa", assets, DefaultSPAConfig())
					if err != nil {
						t.Fatal(err)
					}
					target, expected = "/page", "<main>SPA shell</main>"
				}
				request := httptest.NewRequest("GET", target, nil)
				request.Header.Set("Accept", "text/html")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != 500 || decodeFailure(t, response).Code != InternalError {
					t.Fatal("unsafe asset failure or false SPA fallback")
				}
				if mode == "cycle" && (cycle.visits.Load() == 0 || cycle.visits.Load() > 6*256) {
					t.Fatal("filesystem error inspection exceeded bounds")
				}
				if site == "open" && source.closes.Load() != 1 {
					t.Fatal("failed filesystem open retained the returned body")
				}
				assets.mu.Lock()
				active := assets.active
				assets.mu.Unlock()
				if active != 0 {
					t.Fatal("failed filesystem inspection retained its owner")
				}
				source.failure = nil
				response = httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != 200 || response.Body.String() != expected {
					t.Fatal("healthy asset request failed after recovery")
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				if err := assets.Close(ctx); err != nil {
					t.Fatal("asset close did not finish", err)
				}
			})
		}
	}
}
