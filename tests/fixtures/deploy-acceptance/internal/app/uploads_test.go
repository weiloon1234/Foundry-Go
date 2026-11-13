package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestUploadSettingsPreserveFixtureMigrations(t *testing.T) {
	settings, _, err := fixtureconfig.Load(filepath.Join("..", "..", "config", "deploy.toml"), "")
	if err != nil {
		t.Fatal(err)
	}
	// Build the real fixture declarations without starting services or requiring
	// the cloud credentials supplied by container bootstrap.
	settings.App.Services.Storage.Disks = nil
	settings.App.HTTP.Uploads = http.UploadSettings{Request: http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 4, MaxBytes: 4 * (129 << 20)}, SmallDirectory: t.TempDir(), BodyBytes: 129 << 20, FileBytes: 128 << 20, Timeout: 300 * time.Second, AttachmentMaxActive: 2, StorageTimeout: 210 * time.Second, Staging: http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 2, MaxBytes: 1 << 30}}
	var baseline []infrastructure.MigrationTarget
	for _, enabled := range []bool{false, true, false} {
		settings.App.HTTP.Uploads.Enabled = enabled
		built, err := Builder(settings, work.NewReceipts(t.TempDir())).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		migrations := built.Migrations()
		if err := shutdown(built); err != nil {
			t.Fatal(err)
		}
		if baseline == nil {
			baseline = migrations
		} else if !reflect.DeepEqual(baseline, migrations) {
			t.Fatalf("uploads enabled=%t changed the normal release's migration targets or definitions", enabled)
		}
	}
	for _, expected := range attachments.Migrations() {
		found := false
		for _, target := range baseline {
			for _, definition := range target.Definitions {
				found = found || reflect.DeepEqual(definition, expected)
			}
		}
		if !found {
			t.Fatalf("normal release omitted attachment migration %v", expected.Key)
		}
	}
}

func TestUploadRoutesReleaseCaptureAfterInvalidIdentity(t *testing.T) {
	pool, err := http.NewUploadPool(http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 4, MaxBytes: 4 * (129 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	settings := fixtureconfig.Defaults()
	settings.App.HTTP.Uploads = http.UploadSettings{Enabled: true, Request: http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 4, MaxBytes: 4 * (129 << 20)}, SmallDirectory: t.TempDir(), BodyBytes: 129 << 20, FileBytes: 128 << 20, Timeout: 300 * time.Second, AttachmentMaxActive: 2, StorageTimeout: 210 * time.Second, Staging: http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 2, MaxBytes: 1 << 30}}
	u := &uploadRuntime{pool: pool, settings: settings.App.HTTP.Uploads, backend: &delayedUploadBackend{}, smallBackend: &delayedUploadBackend{}}
	// Assembly and usage need no database connection or provider I/O.
	routes, err := u.routes(application.Services{})
	if err != nil {
		t.Fatal(err)
	}
	router, err := http.NewRouter(routes...)
	if err != nil {
		t.Fatal(err)
	}
	config := http.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	server, err := http.Prepare(router, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	}()
	ready, cancelReady := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	client := &stdhttp.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + address + "/acceptance/uploads/usage")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), `"reserved":0`) {
		t.Fatal(response.StatusCode, string(body), err)
	}
	for _, kind := range []string{"large", "short", "small", "multipart"} {
		request, err := stdhttp.NewRequestWithContext(t.Context(), "POST", "http://"+address+"/acceptance/uploads/"+kind+"?id=invalid", strings.NewReader("--test\r\nContent-Disposition: form-data; name=\"file\"; filename=\"x.txt\"\r\n\r\nx\r\n--test--\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatal("invalid controller identity reached handler dependencies", kind, response.StatusCode)
		}
		if active, reserved := pool.Usage(); active != 0 || reserved != 0 {
			t.Fatal("capture retained after rejection")
		}
	}
}

func TestUploadAdapterHoldsRealPublicationAndStaging(t *testing.T) {
	for _, mode := range []string{"publication", "staging"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			backend, err := local.Open(t.Context(), local.DefaultConfig(root))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			receipts := work.NewReceipts(t.TempDir())
			delayed := &delayedUploadBackend{Backend: backend, receipts: receipts}
			const id = "22512288-ed02-4faa-92f1-b2c69ed383e1"
			key, err := storage.ParseKey("barrier")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), uploadControlKey{}, uploadControl{id, mode}), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := delayed.Put(ctx, key, strings.NewReader("payload"), storage.PutOptions{Size: value.Set[int64](7), Condition: storage.IfAbsent()})
				done <- err
			}()
			deadline := time.Now().Add(time.Second)
			for {
				held, err := receipts.Released(id + ".held")
				if err != nil {
					t.Fatal(err)
				}
				if held {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("barrier was not reached")
				}
				time.Sleep(time.Millisecond)
			}
			if _, err := backend.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
				t.Fatal("barrier published early", err)
			}
			if delayed.active.Load() != 1 || delayed.published.Load() != 0 {
				t.Fatal("incorrect ownership while held")
			}
			if err := receipts.Mark(id + ".release"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if delayed.published.Load() != 1 || delayed.active.Load() != 0 {
				t.Fatal("publication did not complete once")
			}
			reader, _, err := backend.Open(t.Context(), key, storage.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			closeErr := reader.Close()
			if string(data) != "payload" || err != nil || closeErr != nil {
				t.Fatal("stored bytes changed", err, closeErr)
			}
		})
	}
}
