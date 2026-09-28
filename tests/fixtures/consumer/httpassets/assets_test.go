package httpassets_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpassets"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestConsumerLocalAndEmbeddedAssets(t *testing.T) {
	for _, kind := range []string{"embedded", "directory"} {
		t.Run(kind, func(t *testing.T) {
			source, err := httpassets.EmbeddedSource()
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				directory := t.TempDir()
				if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("SPA fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "site.css"), []byte("body { color: navy; }"), 0600); err != nil {
					t.Fatal(err)
				}
				source = foundryhttp.DirectoryAssets(directory)
			}
			app, err := httpassets.Build(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			server, err := foundation.Resolve(app.Services(), httpassets.ServerKey)
			if err != nil {
				t.Fatal(err)
			}
			assets, err := foundation.Resolve(app.Services(), httpassets.AssetsKey)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- app.Run(ctx, foundation.HTTP) }()
			stopped := false
			stop := func() {
				cancel()
				if stopped {
					return
				}
				select {
				case err := <-done:
					stopped = true
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Error("application shutdown", err)
					}
				case <-time.After(5 * time.Second):
					t.Error("application did not drain")
				}
			}
			defer stop()
			readyCtx, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer readyCancel()
			address, err := server.Ready(readyCtx)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			for _, tc := range []struct {
				path, accept string
				status       int
				contains     string
			}{{"/assets/site.css", "*/*", 200, "navy"}, {"/dashboard", "text/html", 200, "SPA fixture"}, {"/missing.js", "text/html", 404, ""}, {"/api/unknown", "text/html", 404, ""}, {"/api/health", "", 204, ""}} {
				request, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+address+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Accept", tc.accept)
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != tc.status || tc.contains != "" && !strings.Contains(string(data), tc.contains) {
					t.Fatal(tc.path, response.StatusCode, string(data))
				}
			}
			stop()
			closedRouter, err := foundryhttp.NewRouter(assets.Mount("closed", "/closed").Register())
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			closedRouter.ServeHTTP(recorder, httptest.NewRequest("GET", "/closed/site.css", nil))
			if recorder.Code != 503 {
				t.Fatal("framework did not close assets on shutdown", recorder.Code)
			}
		})
	}
}
