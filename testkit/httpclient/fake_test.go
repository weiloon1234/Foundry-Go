package httpclient_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/httpclient"
	fakehttp "github.com/weiloon1234/Foundry-Go/testkit/httpclient"
)

func TestFakeOwnsResponsesRecordsAndNeverUsesNetwork(t *testing.T) {
	data := []byte("response")
	headers := http.Header{"X-Test": {"original"}}
	outcome := fakehttp.Respond(200, headers, data)
	data[0] = 'X'
	headers.Set("X-Test", "changed")
	fake, err := fakehttp.New(outcome)
	if err != nil {
		t.Fatal(err)
	}
	config := httpclient.DefaultConfig("fake.test")
	config.Retry = httpclient.NoRetries()
	client, err := httpclient.New(config, fake)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(t.Context())
	response, err := client.Do(t.Context(), client.Post("https://never-contact.invalid/operation").WithBody(httpclient.Bytes([]byte("request"))))
	if err != nil {
		t.Fatal(err)
	}
	text, err := response.Text()
	if err != nil || text != "response" || response.Headers().Get("X-Test") != "original" {
		t.Fatal(response, err)
	}
	record := fake.Requests()[0]
	record.Body()[0] = 'X'
	record.Headers().Set("Changed", "yes")
	if string(fake.Requests()[0].Body()) != "request" || fake.Requests()[0].Headers().Get("Changed") != "" || fake.Pending() != 0 {
		t.Fatal("fake snapshots are mutable")
	}
	if _, err := client.Do(t.Context(), client.Get("https://never-contact.invalid/extra")); !errors.Is(err, fakehttp.Exhausted) {
		t.Fatal("fake exhaustion not preserved", err)
	}
	if fake.Sent() != 2 {
		t.Fatal(fake.Sent())
	}
}

type observedBody struct{ read, closed bool }

func (b *observedBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *observedBody) Close() error             { b.closed = true; return nil }

func TestDirectRoundTripperClosesInvalidRequestsBeforeReading(t *testing.T) {
	for _, mode := range []string{"headers", "url-size", "nil-url"} {
		fake, err := fakehttp.New(fakehttp.Respond(200, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		body := &observedBody{}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://never-contact.invalid/", body)
		if err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "url-size":
			request.URL.Path = strings.Repeat("x", fakehttp.MaxRecordedURLBytes+1)
		case "nil-url":
			request.URL = nil
		default:
			request.Header["X-Too-Many"] = make([]string, 257)
		}
		if _, err := fake.RoundTrip(request); err == nil || body.read || !body.closed || fake.Sent() != 0 || fake.Pending() != 1 {
			t.Fatal("invalid request reached capture", err)
		}
	}
}
func TestFakeRejectsUnboundedOutcomeCopies(t *testing.T) {
	for _, outcome := range []fakehttp.Outcome{fakehttp.Respond(99, nil, nil), fakehttp.Fail(nil), fakehttp.Respond(200, nil, []byte(strings.Repeat("x", fakehttp.MaxRecordedBodyBytes+1))), fakehttp.Respond(200, http.Header{"X-Test": make([]string, 257)}, nil)} {
		if _, err := fakehttp.New(outcome); err == nil {
			t.Fatal("unbounded or invalid fake outcome")
		}
	}
}
