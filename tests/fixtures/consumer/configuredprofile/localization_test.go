package configuredprofile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestConfiguredLocaleAutomaticallyPresentsValidation(t *testing.T) {
	settings := Defaults()
	settings.Features.Locales.Enabled = true
	settings.Features.Locales.Locales = []i18n.LocaleID{"en", "ms"}
	app, err := application.New(settings, quiet()).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Catalog: map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {"validation.non_blank": {Text: "{{attribute}} diperlukan."}, "http.error.validation_failed": {Text: "Pengesahan gagal"}}}}, nil
	}).HTTP(func(application.Services) ([]foundryhttp.RouteRegistration, error) {
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "locale.name", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/name")), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(localization.WelcomeArgsJSON()), foundryhttp.EmptyResponse(204)).WithBodyValidation(localization.WelcomeArgsValidationFields().Name.WithLabel("Nama").Rules(validation.NonBlank[string]()))
		return []foundryhttp.RouteRegistration{endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, localization.WelcomeArgs]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		})}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	t.Cleanup(func() {
		cancel()
		stop(t, app)
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("kernel retained ownership")
		}
	})
	address, err := app.HTTPReady(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	client := &stdhttp.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"name":""}`, 422}, {`{"name":"valid"}`, 204}} {
		request, err := stdhttp.NewRequestWithContext(t.Context(), "POST", "http://"+address+"/name", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept-Language", "ms")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8192))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		if response.StatusCode != tc.status {
			t.Fatal(response.StatusCode, string(body))
		}
		if tc.status == 422 {
			var failure foundryhttp.ErrorResponse
			if err := json.Unmarshal(body, &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Message != "Pengesahan gagal" || len(failure.Issues) != 1 || failure.Issues[0].Message != "Nama diperlukan." || response.Header.Get("Content-Language") != "ms" {
				t.Fatal(failure, response.Header)
			}
		}
	}
}
