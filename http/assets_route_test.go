package http

import (
	"context"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func assetFixture() fstest.MapFS {
	modified := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	return fstest.MapFS{
		"index.html":               {Data: []byte("<main>SPA shell</main>"), ModTime: modified},
		"site.css":                 {Data: []byte("abcdefghij"), ModTime: modified},
		"docs/index.html":          {Data: []byte("documentation"), ModTime: modified},
		"empty":                    {Mode: fs.ModeDir},
		"unknown.bin":              {Data: []byte("binary")},
		".env":                     {Data: []byte("PRIVATE")},
		"nested/.secret":           {Data: []byte("PRIVATE")},
		".well-known/security.txt": {Data: []byte("Contact: hello@example.test")},
	}
}
func assetsForTest(t *testing.T, config AssetsConfig) *Assets {
	t.Helper()
	assets, err := OpenAssets(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := assets.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return assets
}
func TestAssetsServeNativeRangesIndexesAndMedia(t *testing.T) {
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	mount := assets.Mount("assets", "/assets")
	router, err := NewRouter(mount.Register())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, header, value string
		status                      int
		body, media, location       string
	}{
		{"GET", "/assets/site.css", "", "", 200, "abcdefghij", "text/css; charset=utf-8", ""},
		{"HEAD", "/assets/site.css", "", "", 200, "", "text/css; charset=utf-8", ""},
		{"GET", "/assets/site.css", "Range", "bytes=2-5", 206, "cdef", "text/css; charset=utf-8", ""},
		{"GET", "/assets/site.css", "If-Modified-Since", "Thu, 02 Jan 2020 03:04:05 GMT", 304, "", "", ""},
		{"GET", "/assets/", "", "", 200, "<main>SPA shell</main>", "text/html; charset=utf-8", ""},
		{"GET", "/assets/docs/", "", "", 200, "documentation", "text/html; charset=utf-8", ""},
		{"GET", "/assets/docs?lang=en", "", "", 308, "", "", "/assets/docs/?lang=en"},
		{"GET", "/assets/site.css/", "", "", 308, "", "", "/assets/site.css"},
		{"GET", "/assets/unknown.bin", "", "", 200, "binary", "application/octet-stream", ""},
		{"GET", "/assets/missing.js", "", "", 404, "", "application/json", ""},
		{"GET", "/assets/empty/", "", "", 404, "", "application/json", ""},
		{"GET", "/assets/.env", "", "", 404, "", "application/json", ""},
		{"GET", "/assets/nested/.secret", "", "", 404, "", "application/json", ""},
		{"GET", "/assets/.well-known/security.txt", "", "", 200, "Contact: hello@example.test", "text/plain; charset=utf-8", ""},
	} {
		t.Run(tc.method+tc.path+tc.header, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.header != "" {
				request.Header.Set(tc.header, tc.value)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatal(recorder.Code, recorder.Body.String())
			}
			if tc.status < 300 || tc.status == 304 {
				if recorder.Body.String() != tc.body {
					t.Fatal("body", recorder.Body.String())
				}
			}
			if tc.media != "" && recorder.Header().Get("Content-Type") != tc.media {
				t.Fatal("media", recorder.Header())
			}
			if recorder.Header().Get("Location") != tc.location || strings.Contains(recorder.Body.String(), "PRIVATE") {
				t.Fatal("redirect or hidden file", recorder.Header())
			}
			if tc.status < 300 && recorder.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("cache policy missing")
			}
		})
	}
	if len(router.Endpoints()) != 0 {
		t.Fatal("static boundary invented a DTO")
	}
	info := router.Routes()[0]
	if info.Assets == nil || info.Assets.Fallback || !info.Assets.File.Seekable || info.Assets.Prefix != "/assets" {
		t.Fatal(info)
	}
	info.Assets.File.MediaTypes[0] = "changed"
	if router.Routes()[0].Assets.File.MediaTypes[0] == "changed" {
		t.Fatal("metadata not owned")
	}
}

func TestAssetPathsAndConfigSnapshots(t *testing.T) {
	config := DefaultAssetsConfig(FilesystemAssets(assetFixture()))
	assets := assetsForTest(t, config)
	config.Media[".css"] = "image/png"
	if assets.config.media("site.css") != "text/css; charset=utf-8" {
		t.Fatal("caller mutated asset config")
	}
	mount := assets.Mount("web.files", "/files")
	for name, want := range map[AssetPath]string{"": "/files/", "docs/": "/files/docs/", "Résumé notes.css": "/files/R%C3%A9sum%C3%A9%20notes.css"} {
		got, err := mount.URL(name)
		if err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	for _, name := range []AssetPath{"/absolute", "../secret", "a/../secret", "a//b", "a\\b", "a/\x00b"} {
		if _, err := mount.URL(name); err == nil {
			t.Fatal("unsafe path accepted", name)
		}
	}
	for _, prefix := range []string{"", "files", "/files/", "/{wild}", "/a/..", "/a?x=y"} {
		if assets.Mount("web", prefix).Validate() == nil {
			t.Fatal("unsafe prefix", prefix)
		}
	}
	if _, err := NewRouter(mount.Register(), assets.Mount("web.files", "/other").Register()); err == nil {
		t.Fatal("duplicate asset ID accepted")
	}
	router, err := NewRouter(mount.Register())
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/files/%2e%2e/secret", nil))
	if recorder.Code == 200 {
		t.Fatal("traversal served")
	}
}

func TestDirectoryAssetsConfineSymlinksAndRegularFiles(t *testing.T) {
	directory := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ok.txt"), []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("PRIVATE-EXTERNAL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(directory, "escape.txt")); err != nil {
		t.Skip(err)
	}
	assets := assetsForTest(t, DefaultAssetsConfig(DirectoryAssets(directory)))
	router, err := NewRouter(assets.Mount("files", "/files").Register())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ok.txt", "escape.txt"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/files/"+name, nil))
		if name == "ok.txt" {
			if recorder.Code != 200 || recorder.Body.String() != "public" {
				t.Fatal("local asset", recorder.Code)
			}
		} else if recorder.Code == 200 || strings.Contains(recorder.Body.String(), "PRIVATE") || strings.Contains(recorder.Body.String(), outside) {
			t.Fatal("root escaped", recorder.Code, recorder.Body.String())
		}
	}
}
