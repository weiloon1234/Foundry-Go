package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func FuzzAssetPathAndSPABoundaries(f *testing.F) {
	f.Add("site.css", "text/html")
	f.Add("nested/.secret", "text/html")
	f.Add("../.env", "*/*")
	f.Add("Résumé notes.css", "text/html; note=\"a,b\"")
	f.Fuzz(func(t *testing.T, name, accept string) {
		if len(name) > 1024 || len(accept) > 4096 {
			return
		}
		assets, err := OpenAssets(t.Context(), DefaultAssetsConfig(FilesystemAssets(assetFixture())))
		if err != nil {
			t.Fatal(err)
		}
		defer assets.Close(context.Background())
		mount := assets.Mount("files", "/files")
		location, err := mount.URL(AssetPath(name))
		if err != nil {
			return
		}
		router, err := NewRouter(mount.Register())
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", location, nil)
		request.Header.Set("Accept", accept)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if strings.Contains(recorder.Body.String(), "PRIVATE") {
			t.Fatal("hidden source escaped")
		}
		if redirect := recorder.Header().Get("Location"); redirect != "" && (!strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//")) {
			t.Fatal("non-local redirect")
		}
		// Exercise bounded, quote-aware HTML navigation independently of URL syntax.
		_ = htmlNavigation([]string{accept}, name)
	})
}
