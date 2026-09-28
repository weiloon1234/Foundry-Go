package configuredprofile

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"io"
	stdhttp "net/http"
	"time"
)

// Smoke starts the same application/kernel as serve mode, performs one real
// loopback HTTP request, then drains it. It requires no external services.
func Smoke(ctx context.Context, app *application.App) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	defer func() {
		cancel()
		cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		err = errors.Join(err, app.Shutdown(cleanup))
		select {
		case stopped := <-done:
			// Run reports caller cancellation. Shutdown above separately retains
			// all task/cleanup failures, including failures joined to cancellation.
			if !errors.Is(stopped, context.Canceled) {
				err = errors.Join(err, stopped)
			}
		case <-cleanup.Done():
			err = errors.Join(err, cleanup.Err())
		}
	}()
	address, err := app.HTTPReady(ctx)
	if err != nil {
		return err
	}
	request, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, "http://"+address+"/profile", nil)
	if err != nil {
		return err
	}
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64))
	err = errors.Join(readErr, response.Body.Close())
	if err != nil {
		return err
	}
	if response.StatusCode != 200 || string(body) != "profile" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		return fault.New(fault.Invalid, "configured profile returned an unexpected response")
	}
	return nil
}
