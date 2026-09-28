package httpsigned_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpsigned"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type previewService struct {
	calls    atomic.Int32
	received chan httpsigned.PreviewRequest
}

func (s *previewService) Preview(_ context.Context, in httpsigned.PreviewRequest) (httpdto.UserResponse, error) {
	s.calls.Add(1)
	s.received <- in
	return httpdto.UserResponse{ID: in.Path.User, Email: "preview@example.test", State: models.StatusActive}, nil
}
func TestTemporaryTypedLinksOverNativeHTTP(t *testing.T) {
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New(strings.Repeat("signed-url-fixture", 3))})
	if err != nil {
		t.Fatal(err)
	}
	now := testkit.NewClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	signer, err := foundryhttp.NewURLSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	links, err := httpsigned.NewLinks(signer)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	origin, err := foundryhttp.ParseOrigin("http://" + server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	service := &previewService{received: make(chan httpsigned.PreviewRequest, 4)}
	server.Config.Handler, err = links.Handler(service, foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{origin}})
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	client := server.Client()
	client.Timeout = 3 * time.Second
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	query := httpquery.SearchInput{User: id, Search: value.Set("中文 + a/b"), Statuses: []models.Status{models.StatusActive, models.StatusDisabled}}
	link, err := links.PreviewURL(t.Context(), origin, httpkernel.UserPath{User: id}, query, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), "GET", link, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var dto httpdto.UserResponse
		err = json.NewDecoder(response.Body).Decode(&dto)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || dto.ID != id || dto.Email != "preview@example.test" {
			t.Fatalf("typed signed request failed: %d %v", response.StatusCode, err)
		}
		got := <-service.received
		term, _ := got.Query.Search.Get()
		if got.Path.User != id || got.Query.User != id || term != "中文 + a/b" || len(got.Query.Statuses) != 2 {
			t.Fatal("concrete model/query values changed")
		}
	}
	asset, err := links.AssetURL(t.Context(), origin, httpkernel.AssetPath{File: "reports/中文 + draft.csv"}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(asset)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(data) != "reports/中文 + draft.csv" {
		t.Fatal("typed raw path did not round trip")
	}
	for _, bad := range []string{link + "&extra=1", strings.Replace(link, "signature=v1.", "signature=v2.", 1)} {
		response, err := client.Get(bad)
		if err != nil {
			t.Fatal(err)
		}
		var failure foundryhttp.ErrorResponse
		err = json.NewDecoder(response.Body).Decode(&failure)
		response.Body.Close()
		if err != nil || response.StatusCode != 403 || failure.Code != foundryhttp.Forbidden {
			t.Fatal("tampered link did not use shared 403 contract")
		}
	}
	now.Advance(time.Minute)
	response, err = client.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 403 || service.calls.Load() != 2 {
		t.Fatal("expired or tampered link reached domain service")
	}
	description, err := links.Preview.Description()
	if err != nil || description.Route.SignedURL == nil || len(description.Query) != 3 || description.Response.Schema.Root != "foundry.test/consumer/httpdto.UserResponse" {
		t.Fatal("runtime signing and DTO metadata diverged")
	}
}
