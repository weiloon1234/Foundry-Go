package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weiloon1234/Foundry-Go/deployment"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// featureRelay is the disposable custom service embedded in the fixture artifact.
func featureRelay(ctx context.Context, args []string) error {
	if len(args) < 2 || len(args) > 4 {
		return fmt.Errorf("feature relay requires an address, port and optional Unix path")
	}
	if len(args) >= 3 {
		if err := featureUnixBinding(ctx, args[2]); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(args[0], args[1]))
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shared" && len(args) == 4 {
			if err := featureSharedReader(args[3]); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, "shared-read-only;writer-enospc")
			return
		}
		if r.URL.Path != "/ready" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "feature-relay-ready")
	})}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-stopped:
		}
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func featureBindingProbe() (func(context.Context) error, error) {
	bindings, present, err := deployment.LoadBindings()
	if err != nil {
		return nil, err
	}
	if !present {
		return func(context.Context) error { return nil }, nil
	}
	if directory, err := bindings.SharedDirectory("render-cache"); err == nil {
		if err := featureSharedWriter(directory); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fault.Missing) {
		return nil, err
	}
	endpoint, err := bindings.Endpoint("relay")
	if err != nil {
		return nil, err
	}
	tool, err := bindings.Executable("checksum")
	if err != nil || tool.Path == "" {
		return nil, fmt.Errorf("checksum binding unavailable")
	}
	public, err := bindings.PublicListener("relay_public")
	if err != nil || public.Role != "relay" || public.Listener != "control" || public.Port != 8554 ||
		public.TLS != deployment.TLSNone || public.Transport != deployment.TCPTransport ||
		!strings.HasPrefix(public.Hostname, "relay.") || !strings.HasSuffix(public.Hostname, ".acceptance.invalid") || len(public.PublicAddresses) != 2 {
		return nil, fmt.Errorf("public relay binding unavailable or incorrect")
	}
	target := endpoint.URL()
	target.Path = "/ready"
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 128))
		if err != nil || response.StatusCode != 200 || string(body) != "feature-relay-ready" {
			return fmt.Errorf("rendered relay binding did not reach the custom service")
		}
		return nil
	}, nil
}

func featureUnix(ctx context.Context, args []string) error {
	if len(args) != 1 || filepath.Dir(args[0]) != os.Getenv("XDG_RUNTIME_DIR") {
		return fmt.Errorf("Unix provider requires its delivered socket path and directory")
	}
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return fmt.Errorf("Unix provider requires bindings")
	}
	if _, err := bindings.SharedDirectory("render-cache"); !errors.Is(err, fault.Missing) {
		return fmt.Errorf("unlisted role received shared directory")
	}
	forbidden := "/run/foundry/services/" + string(bindings.Unit().Instance) + "/shared/render-cache/frame.txt"
	if _, err := os.ReadFile(forbidden); err == nil {
		return fmt.Errorf("unlisted role could read shared directory")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: args[0]})
	if err != nil {
		return err
	}
	listener.SetUnlinkOnClose(false) // Restart must remove this declared stale socket.
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		var request [4]byte
		if _, err := io.ReadFull(connection, request[:]); err == nil {
			switch string(request[:]) {
			case "ping":
				_, _ = io.WriteString(connection, "unix-ready")
			case "deny":
				if _, err := os.ReadFile(forbidden); errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
					_, _ = io.WriteString(connection, "denied")
				}
			}
		}
		_ = connection.Close()
	}
}

func featureSharedWriter(directory deployment.SharedDirectory) error {
	if directory.Access != deployment.DirectoryReadWrite || directory.MaxBytes != 1<<20 {
		return fmt.Errorf("shared writer binding incorrect")
	}
	if err := os.WriteFile(filepath.Join(directory.Path, "frame.txt"), []byte("prepared-media"), 0600); err != nil {
		return err
	}
	path := filepath.Join(directory.Path, "bound-probe")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	block := make([]byte, 65536)
	var writeErr error
	for written := int64(0); written <= directory.MaxBytes; written += int64(len(block)) {
		if _, writeErr = file.Write(block); writeErr != nil {
			break
		}
	}
	closeErr := file.Close()
	if !errors.Is(writeErr, syscall.ENOSPC) || closeErr != nil {
		return fmt.Errorf("shared directory did not enforce ENOSPC: %v, %v", writeErr, closeErr)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory.Path, "bound-proof"), []byte("ENOSPC"), 0600)
}

func featureSharedReader(path string) error {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return fmt.Errorf("shared reader requires bindings")
	}
	directory, err := bindings.SharedDirectory("render-cache")
	if err != nil || directory.Access != deployment.DirectoryReadOnly || directory.Path != path ||
		path != os.Getenv("RENDER_CACHE") {
		return fmt.Errorf("shared descriptor, argv and environment disagree")
	}
	for name, expected := range map[string]string{"frame.txt": "prepared-media", "bound-proof": "ENOSPC"} {
		data, err := os.ReadFile(filepath.Join(path, name))
		if err != nil || string(data) != expected {
			return fmt.Errorf("shared reader did not read writer output")
		}
	}
	for _, name := range []string{"reader-write", "frame.txt"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte("forbidden"), 0600); !errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("reader write did not fail with EROFS: %v", err)
		}
	}
	return nil
}

func featureUnixBinding(ctx context.Context, path string) error {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return fmt.Errorf("Unix consumer requires its runtime descriptor")
	}
	endpoint, err := bindings.Endpoint("socket")
	if err != nil || endpoint.Network() != "unix" || endpoint.Path != path ||
		endpoint.Directory != os.Getenv("XDG_RUNTIME_DIR") || endpoint.Name != os.Getenv("WAYLAND_DISPLAY") {
		return fmt.Errorf("Unix descriptor, argv and environment disagree")
	}
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, endpoint.Network(), endpoint.Path)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	if _, err := io.WriteString(connection, "ping"); err != nil {
		return err
	}
	var response [10]byte
	if _, err := io.ReadFull(connection, response[:]); err != nil || string(response[:]) != "unix-ready" {
		return fmt.Errorf("Unix binding did not reach its provider")
	}
	return nil
}
