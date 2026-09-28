package httpcompression_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpcompression"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	"github.com/andybalholm/brotli"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{ reply string }

func (s service) Update(_ context.Context, in httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	return httpdto.UserResponse{ID: in.Path.User, Email: s.reply, State: models.StatusActive}, nil
}

func TestTypedEndpointCompressionOverNativeHTTP(t *testing.T) {
	text := strings.Repeat("typed response text ", 200)
	handler, err := httpcompression.Handler(service{reply: text})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	path, err := httpendpoints.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{Latitude: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	for _, coding := range []string{"gzip", "br", "identity", "*;q=0"} {
		request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+path, strings.NewReader(`{"email":"member@example.test"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept-Encoding", coding)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("compression interrupted the native response: %v", err)
		}
		if coding == "*;q=0" {
			var failure foundryhttp.ErrorResponse
			err = json.NewDecoder(response.Body).Decode(&failure)
			response.Body.Close()
			if err != nil || response.StatusCode != 406 || failure.Code != foundryhttp.NotAcceptable {
				t.Fatalf("negotiation error was not delivered cleanly: %+v %v", failure, err)
			}
			continue
		}
		var reader io.Reader = response.Body
		var closeDecoder func() error
		switch coding {
		case "gzip":
			decoder, err := gzip.NewReader(reader)
			if err != nil {
				response.Body.Close()
				t.Fatal(err)
			}
			reader = decoder
			closeDecoder = decoder.Close
		case "br":
			reader = brotli.NewReader(reader)
		}
		var reply httpdto.UserResponse
		err = json.NewDecoder(reader).Decode(&reply)
		if closeDecoder != nil {
			_ = closeDecoder()
		}
		response.Body.Close()
		want := coding
		if want == "identity" {
			want = ""
		}
		if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Encoding") != want || reply.ID != id || reply.Email != text || reply.State != models.StatusActive {
			t.Fatalf("typed response changed through %s: %d %v", coding, response.StatusCode, err)
		}
	}
	names := httpcompression.EncodingNames()
	if len(names) != 2 || names[0] != foundryhttp.BrotliEncoding || names[1] != foundryhttp.GzipEncoding {
		t.Fatal("typed encoding metadata changed")
	}
}
