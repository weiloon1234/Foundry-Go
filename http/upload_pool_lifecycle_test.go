package http

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
)

func uploadLifecycleRouter(t *testing.T, directory string) (*Router, *UploadPool) {
	t.Helper()
	pool, err := NewUploadPool(UploadPoolConfig{Directory: directory, MaxActive: 1, MaxBytes: DefaultEndpointLimits().Multipart.Bytes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	endpoint := admittedEndpoint(t, pool)
	second := admittedEndpoint(t, pool)
	second.route = DefineRoute(RouteSpec{ID: "uploads.second", Method: POST, Access: Public}, StaticPath("/uploads-second"), BodyLimit(endpoint.limits.Multipart.Bytes), Timeout(time.Minute))
	handle := func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }
	router, err := NewRouter(endpoint.Handle(handle), second.Handle(handle))
	if err != nil {
		t.Fatal(err)
	}
	return router, pool
}

func prepareUploadLifecycleServer(t *testing.T, handler stdhttp.Handler) *Server {
	t.Helper()
	// Exercise inventory preservation through more than one middleware wrapper.
	handler, err := ApplyMiddleware(handler, Compression(DefaultCompressionConfig()))
	if err != nil {
		t.Fatal(err)
	}
	handler, err = ApplyMiddleware(handler, ETags(DefaultETagConfig()))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	server, err := Prepare(handler, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func runUploadLifecycleServer(t *testing.T, server *Server) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	stop := func() {
		cancel()
		select {
		case <-server.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("upload server did not drain")
		}
	}
	t.Cleanup(stop)
	ready, cancelReady := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	return "http://" + address, func() {
		stop()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestUploadPoolInspectionBuildsLeaveServingAvailable(t *testing.T) {
	directory := t.TempDir()
	for range 5 {
		router, _ := uploadLifecycleRouter(t, directory)
		_ = router.Routes()                         // route:list
		_ = prepareUploadLifecycleServer(t, router) // configuration validation
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("inspection claimed the directory", entries, err)
		}
	}
	router, pool := uploadLifecycleRouter(t, directory)
	origin, stop := runUploadLifecycleServer(t, prepareUploadLifecycleServer(t, router))
	wire, media := multipartWire(t, primaryUpload("healthy"))
	client := &stdhttp.Client{Timeout: 3 * time.Second}
	response, err := client.Post(origin+"/uploads", media, bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("serve after inspection failed", response.StatusCode)
	}
	stop()
	if err := pool.Close(); err != nil {
		t.Fatal("application close after drain failed", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".foundry-upload-owner")); !os.IsNotExist(err) {
		t.Fatal("drain retained marker", err)
	}
}

const uploadOwnerTestDirectory = "FOUNDRY_UPLOAD_POOL_TEST_DIRECTORY"

func TestUploadPoolOwnerProcess(t *testing.T) {
	directory := os.Getenv(uploadOwnerTestDirectory)
	if directory == "" {
		return
	}
	router, pool := uploadLifecycleRouter(t, directory)
	_, _ = runUploadLifecycleServer(t, prepareUploadLifecycleServer(t, router))
	reservation, err := pool.pool.Reserve(t.Context(), 128)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := upload.New(t.Context(), upload.Config{TempDirectory: directory, MaxBytes: 128, MaxFileBytes: 128, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Attach(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Capture(t.Context(), strings.NewReader("orphaned capture"), "file", ""); err != nil {
		t.Fatal(err)
	}
	fmt.Println("upload-owner-ready")
	select {} // The parent kills the process with a live reservation and lock.
}

func startUploadOwnerProcess(t *testing.T, directory string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestUploadPoolOwnerProcess$")
	command.Env = append(os.Environ(), uploadOwnerTestDirectory+"="+directory)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	ready := make(chan bool, 1)
	go func() {
		line, _ := bufio.NewReader(output).ReadString('\n')
		ready <- line == "upload-owner-ready\n"
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("owner subprocess failed before claiming")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner subprocess did not become ready")
	}
	return command
}

func TestUploadPoolLiveOwnerRefusesAndCrashReconciles(t *testing.T) {
	directory := t.TempDir()
	owner := startUploadOwnerProcess(t, directory)
	router, pool := uploadLifecycleRouter(t, directory)
	server := prepareUploadLifecycleServer(t, router)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := server.Run(ctx); !errors.Is(err, fault.Conflict) {
		t.Fatal("second live server was not refused", err)
	}
	if address, err := server.Ready(ctx); address != "" || !errors.Is(err, fault.Conflict) {
		t.Fatal("refused server reported ready", address, err)
	}
	if _, err := pool.pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Closed) {
		t.Fatal("refused server admitted work", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("live owner's capture was changed", entries, err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err == nil {
		t.Fatal("owner did not crash")
	}
	_, stop := runUploadLifecycleServer(t, prepareUploadLifecycleServer(t, router))
	entries, err = os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".foundry-upload-owner" {
		t.Fatal("crash captures were not reconciled", entries, err)
	}
	reservation, err := pool.pool.Reserve(t.Context(), pool.pool.MaxBytes())
	if err != nil {
		t.Fatal("reconciled capacity is unavailable", err)
	}
	reservation.Release()
	stop()
}

func TestUploadPoolListenerFailureReleasesClaim(t *testing.T) {
	directory := t.TempDir()
	router, _ := uploadLifecycleRouter(t, directory)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := prepareUploadLifecycleServer(t, router)
	server.config.Address = listener.Addr().String()
	if err := server.Run(t.Context()); err == nil {
		t.Fatal("occupied listener unexpectedly started")
	}
	fresh, _ := uploadLifecycleRouter(t, directory)
	_, stop := runUploadLifecycleServer(t, prepareUploadLifecycleServer(t, fresh))
	stop()
}

func TestUploadPoolClaimFailureClosesOnlyThisServersClaims(t *testing.T) {
	firstDirectory, heldDirectory := t.TempDir(), t.TempDir()
	heldRouter, heldPool := uploadLifecycleRouter(t, heldDirectory)
	_, stop := runUploadLifecycleServer(t, prepareUploadLifecycleServer(t, heldRouter))
	defer stop()
	_, firstPool := uploadLifecycleRouter(t, firstDirectory)
	first := admittedEndpoint(t, firstPool)
	second := admittedEndpoint(t, heldPool)
	second.route = DefineRoute(RouteSpec{ID: "uploads.held", Method: POST, Access: Public}, StaticPath("/held"), BodyLimit(second.limits.Multipart.Bytes), Timeout(time.Minute))
	handle := func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }
	router, err := NewRouter(first.Handle(handle), second.Handle(handle))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareUploadLifecycleServer(t, router).Run(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("same pool was claimed by two servers", err)
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, ".foundry-upload-owner")); !os.IsNotExist(err) {
		t.Fatal("partial startup retained its first claim", err)
	}
	reservation, err := heldPool.pool.Reserve(t.Context(), 1)
	if err != nil {
		t.Fatal("failed startup closed the other server's pool", err)
	}
	reservation.Release()
}

func TestUploadPoolApplicationShutdownCloseIsIdempotent(t *testing.T) {
	directory := t.TempDir()
	router, pool := uploadLifecycleRouter(t, directory)
	key := foundation.NewKey[*Server]("upload.server")
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	closed := false
	app, err := foundation.NewBuilder(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(
		foundation.Module{Name: "uploads", OnBoot: func(_ context.Context, runtime *foundation.Runtime) error {
			return runtime.OnShutdown("pool", func(context.Context) error {
				closed = true
				return pool.Close()
			})
		}},
		Module("http", key, config, func(foundation.Resolver) (stdhttp.Handler, error) { return router, nil }),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	server, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Run(t.Context(), foundation.HTTP) }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := server.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("application shutdown hook was not called")
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}
