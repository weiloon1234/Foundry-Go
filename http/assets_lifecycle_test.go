package http

import (
	"context"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

type countingAssetFS struct {
	fstest.MapFS
	stats, opens int
}

func (f *countingAssetFS) Stat(name string) (fs.FileInfo, error) {
	f.stats++
	return f.MapFS.Stat(name)
}
func (f *countingAssetFS) Open(name string) (fs.File, error) { f.opens++; return f.MapFS.Open(name) }

func TestAssetCloseWaitsForBodyAndCanBeRetried(t *testing.T) {
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	prepared, err := DownloadResponse("text/css; charset=utf-8").prepare(t.Context(), assets.Download("site.css"), DefaultEndpointLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanup()()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := assets.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("close did not wait for body", err)
	}
	select {
	case <-assets.done:
		t.Fatal("source closed with active body")
	default:
	}
	if _, err := assets.stat(t.Context(), "site.css"); !errors.Is(err, Unavailable) {
		t.Fatal("new lookup admitted during close", err)
	}
	data := make([]byte, 3)
	if n, err := prepared.file.reader.Read(data); err != nil || n != 3 || string(data) != "abc" {
		t.Fatal("close interrupted existing body", n, err)
	}
	if err := prepared.cleanup()(); err != nil {
		t.Fatal(err)
	}
	if err := assets.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-assets.done:
	default:
		t.Fatal("last body did not finish close")
	}
}

func TestAssetsModuleOpensOnlyAtBootAndCleansUpAfterFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "later-boot-failure"}[fail], func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "created-after-build")
			key := foundation.NewKey[*Assets]("web.assets")
			failure := errors.New("later provider failed")
			inspect := foundation.Module{Name: "inspect", Requires: []foundation.ProviderID{"assets"}, OnRegister: func(r *foundation.Registrar) error {
				return r.Kernel(foundation.CLI, func(rt *foundation.Runtime) (foundation.Kernel, error) {
					assets, err := foundation.Resolve(rt.Services(), key)
					if err != nil {
						return nil, err
					}
					return foundation.KernelFunc(func(ctx context.Context) error {
						router, err := NewRouter(assets.Mount("files", "/files").Register())
						if err != nil {
							return err
						}
						recorder := httptest.NewRecorder()
						router.ServeHTTP(recorder, httptest.NewRequest("GET", "/files/ok.txt", nil))
						if recorder.Code != 200 || recorder.Body.String() != "booted" {
							t.Error("provider was not open during kernel", recorder.Code)
						}
						return nil
					}), nil
				})
			}, OnBoot: func(context.Context, *foundation.Runtime) error {
				if fail {
					return failure
				}
				return nil
			}}
			app, err := foundation.NewBuilder().Register(AssetsModule("assets", key, DefaultAssetsConfig(DirectoryAssets(directory))), inspect).Build(t.Context())
			if err != nil {
				t.Fatal("Build performed filesystem I/O", err)
			}
			assets, err := foundation.Resolve(app.Services(), key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := assets.stat(t.Context(), "ok.txt"); !errors.Is(err, Unavailable) {
				t.Fatal("assets opened before Boot", err)
			}
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "ok.txt"), []byte("booted"), 0600); err != nil {
				t.Fatal(err)
			}
			err = app.Run(t.Context(), foundation.CLI)
			if fail && !errors.Is(err, failure) || !fail && err != nil {
				t.Fatal("lifecycle result", err)
			}
			if _, err := assets.stat(t.Context(), "ok.txt"); !errors.Is(err, Unavailable) {
				t.Fatal("source survived application shutdown", err)
			}
			if _, err := assets.root.Stat("ok.txt"); !errors.Is(err, os.ErrClosed) {
				t.Fatal("directory handle retained", err)
			}
		})
	}
}

func TestAssetFailedStatReleasesLease(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(panickingAssetFS{mode: mode})))
			if _, err := assets.stat(t.Context(), "anything"); err == nil {
				t.Fatal("broken filesystem accepted")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := assets.Close(ctx); err != nil {
				t.Fatal("failed callback retained its lease", err)
			}
		})
	}
}

type panickingAssetFS struct{ mode string }

func (f panickingAssetFS) Open(string) (fs.File, error) {
	if f.mode == "goexit" {
		runtime.Goexit()
	}
	panic("private-filesystem-detail")
}
