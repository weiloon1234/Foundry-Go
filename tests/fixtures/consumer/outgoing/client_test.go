package outgoing_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
	fakehttp "github.com/weiloon1234/Foundry-Go/testkit/httpclient"
)

func TestNamedHTTPClientUsesGeneratedCodecsAndExplicitFake(t *testing.T) {
	fake, err := fakehttp.New(fakehttp.Respond(201, http.Header{"Content-Type": {"application/json"}}, []byte(`{"id":"9007199254740993","name":"Ada"}`)), fakehttp.Respond(200, nil, []byte("stream")))
	if err != nil {
		t.Fatal(err)
	}
	config := httpclient.DefaultConfig("accounts.api")
	config.BaseURL = "https://accounts.example.test/api"
	client, err := httpclient.New(config, fake)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())
	service := outgoing.NewAccounts(client)
	receipt, err := service.Create(t.Context(), "Ada", secret.New("private-token"))
	if err != nil || receipt.ID != 9007199254740993 || receipt.Name != "Ada" {
		t.Fatal(receipt, err)
	}
	recorded := fake.Requests()[0]
	if recorded.Method() != http.MethodPost || recorded.URL() != "https://accounts.example.test/api/accounts" || recorded.Headers().Get("Authorization") != "Bearer private-token" || !strings.Contains(string(recorded.Body()), `"name":"Ada"`) {
		t.Fatal("request contract changed")
	}
	var retained *httpclient.StreamResponse
	if err := service.Download(t.Context(), func(_ context.Context, response *httpclient.StreamResponse) error {
		retained = response
		data, err := io.ReadAll(response)
		if string(data) != "stream" {
			t.Error("stream content changed")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("stream escaped callback", err)
	}
	if fake.Sent() != 2 || fake.Pending() != 0 {
		t.Fatal("fake request count")
	}
}

func TestResponseDecodeRejectsWireMismatchWithoutLosingExactID(t *testing.T) {
	for _, body := range []string{`{"id":9007199254740993,"name":"Ada"}`, `{"id":"1","name":"Ada","extra":true}`, `{"id":"1","id":"2","name":"Ada"}`} {
		fake, err := fakehttp.New(fakehttp.Respond(200, nil, []byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		config := httpclient.DefaultConfig("accounts.api")
		config.BaseURL = "https://accounts.example.test"
		client, err := httpclient.New(config, fake)
		if err != nil {
			t.Fatal(err)
		}
		_, err = outgoing.NewAccounts(client).Create(t.Context(), "Ada", secret.New("token"))
		var classified *httpclient.Error
		if !errors.As(err, &classified) || classified.Kind() != httpclient.DecodeFailed {
			t.Fatal("unvalidated response accepted", err)
		}
		if err := client.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
