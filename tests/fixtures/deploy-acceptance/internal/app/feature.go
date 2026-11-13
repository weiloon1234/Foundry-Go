package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/deployment"
)

// featureRelay is the disposable custom service embedded in the fixture artifact.
func featureRelay(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("feature relay requires an address and port")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(args[0], args[1]))
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	endpoint, err := bindings.Endpoint("relay")
	if err != nil {
		return nil, err
	}
	tool, err := bindings.Executable("checksum")
	if err != nil || tool.Path == "" {
		return nil, fmt.Errorf("checksum binding unavailable")
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
