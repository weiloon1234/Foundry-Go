package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestAssetsModuleServesReplacedDirectory(t *testing.T) {
	for _, replacement := range []string{"rename", "remove-and-recreate", "temporarily-missing", "reopen-failure"} {
		t.Run(replacement, func(t *testing.T) {
			parent := t.TempDir()
			directory := filepath.Join(parent, "_assets")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "old.js"), []byte("old bundle"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(directory, "changed.js"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("PRIVATE"), 0600); err != nil {
				t.Fatal(err)
			}
			key := foundation.NewKey[*Assets]("replacement.assets")
			config := DefaultAssetsConfig(DirectoryAssets(directory))
			config.CacheControl = "public, max-age=31536000, immutable"
			var assets *Assets
			inspect := foundation.Module{Name: "inspect", Requires: []foundation.ProviderID{"assets"}, OnRegister: func(r *foundation.Registrar) error {
				return r.Kernel(foundation.CLI, func(rt *foundation.Runtime) (foundation.Kernel, error) {
					var err error
					assets, err = foundation.Resolve(rt.Services(), key)
					if err != nil {
						return nil, err
					}
					return foundation.KernelFunc(func(ctx context.Context) (failure error) {
						defer func() {
							if failure != nil {
								t.Error(failure)
							}
						}()
						router, err := NewRouter(assets.Mount("files", "/files").Register())
						if err != nil {
							return err
						}
						request := func(path string) *httptest.ResponseRecorder {
							recorder := httptest.NewRecorder()
							router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
							return recorder
						}
						if got := request("/files/old.js"); got.Code != 200 || got.Body.String() != "old bundle" {
							return fmt.Errorf("initial bundle: status %d, body %q", got.Code, got.Body.String())
						}
						if got := request("/files/changed.js/"); got.Code != 404 {
							return fmt.Errorf("initial empty directory: status %d", got.Code)
						}
						body, err := assets.open(ctx, "old.js")
						if err != nil {
							return err
						}
						defer body.Body.Close()
						oldRoot := assets.root
						if replacement != "remove-and-recreate" {
							err = os.Rename(directory, filepath.Join(parent, "previous"))
						} else {
							err = os.RemoveAll(directory)
						}
						if err != nil {
							return err
						}
						if replacement == "reopen-failure" {
							if err := os.WriteFile(directory, []byte("not a directory"), 0600); err != nil {
								return err
							}
						}
						if replacement == "temporarily-missing" || replacement == "reopen-failure" {
							time.Sleep(time.Second + 10*time.Millisecond)
							for range 2 {
								if got := request("/files/old.js"); got.Code != 200 || got.Body.String() != "old bundle" {
									return fmt.Errorf("failed refresh prevented serving old root: status %d, body %q", got.Code, got.Body.String())
								}
							}
							if got := request("/files/new.js"); got.Code != 404 {
								return fmt.Errorf("new bundle absent from old root: status %d", got.Code)
							}
						}
						if replacement == "reopen-failure" {
							if err := os.Remove(directory); err != nil {
								return err
							}
						}
						if err := os.Mkdir(directory, 0700); err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(directory, "new.js"), []byte("new bundle"), 0600); err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(directory, "changed.js"), []byte("now a file"), 0600); err != nil {
							return err
						}
						if err := os.Symlink("../secret.txt", filepath.Join(directory, "escape.txt")); err != nil {
							return err
						}
						deadline := time.Now().Add(3 * time.Second)
						for {
							got := request("/files/new.js")
							if got.Code == 200 && got.Body.String() == "new bundle" {
								if got.Header().Get("Cache-Control") != string(config.CacheControl) {
									return fmt.Errorf("cache policy changed: %q", got.Header().Get("Cache-Control"))
								}
								break
							}
							if time.Now().After(deadline) {
								return fmt.Errorf("replacement bundle: status %d, body %q", got.Code, got.Body.String())
							}
							time.Sleep(10 * time.Millisecond)
						}
						if got := request("/files/old.js"); got.Code != 404 {
							return fmt.Errorf("retired bundle still served: %d", got.Code)
						}
						if got := request("/files/changed.js"); got.Code != 200 || got.Body.String() != "now a file" {
							return fmt.Errorf("retired directory metadata reused: status %d", got.Code)
						}
						for _, path := range []string{"/files/escape.txt", "/files/%2e%2e/secret.txt"} {
							got := request(path)
							if got.Code == 200 || strings.Contains(got.Body.String(), "PRIVATE") || strings.Contains(got.Body.String(), parent) {
								return fmt.Errorf("replacement root escaped: status %d", got.Code)
							}
						}
						if _, err := oldRoot.Stat("."); err != nil {
							return fmt.Errorf("leased root closed early: %w", err)
						}
						data, err := io.ReadAll(body.Body)
						if err != nil || string(data) != "old bundle" {
							return fmt.Errorf("replacement interrupted old body: %q, %v", data, err)
						}
						if err := body.Body.Close(); err != nil {
							return err
						}
						if _, err := oldRoot.Stat("."); !errors.Is(err, os.ErrClosed) {
							return fmt.Errorf("retired root retained after last body: %v", err)
						}
						if got := request("/files/new.js"); got.Code != 200 {
							return fmt.Errorf("healthy request after escape rejection: %d", got.Code)
						}
						return nil
					}), nil
				})
			}}
			app, err := foundation.NewBuilder().Register(AssetsModule("assets", key, config), inspect).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := app.Run(t.Context(), foundation.CLI); err != nil {
				t.Fatal(err)
			}
			if _, err := assets.root.Stat("."); !errors.Is(err, os.ErrClosed) {
				t.Fatal("replacement root survived module shutdown", err)
			}
		})
	}
}
