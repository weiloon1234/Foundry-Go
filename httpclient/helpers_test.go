package httpclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
	fakehttp "github.com/weiloon1234/Foundry-Go/testkit/httpclient"
)

func TestEmptyBodiesAreSentWithoutChunkingOrProbes(t *testing.T) {
	var failures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A strict server rejects chunked uploads without a length (411).
		if len(r.TransferEncoding) != 0 || r.ContentLength != 0 {
			failures.Add(1)
			w.WriteHeader(http.StatusLengthRequired)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(server.Close)
	config := testConfig()
	config.BaseURL = server.URL
	c := newClient(t, config, nil)
	for _, request := range []httpclient.Request{c.Get("read"), c.Post("empty"), c.Post("empty").WithBody(httpclient.Bytes(nil)), c.Request(http.MethodDelete, "resource")} {
		response, err := c.Do(t.Context(), request)
		if err != nil || response.Status() != 204 {
			t.Fatal("empty body request failed", err)
		}
	}
	if failures.Load() != 0 {
		t.Fatal("empty bodies were chunked or lacked a zero length", failures.Load())
	}
}

func TestFormMultipartAndBasicAuthHelpers(t *testing.T) {
	fake := newFake(t, fakehttp.Respond(200, nil, nil), fakehttp.Respond(200, nil, nil), fakehttp.Respond(200, nil, nil))
	c := newClient(t, testConfig(), fake)
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"read write"}}
	if _, err := c.Do(t.Context(), c.Post("token").Form(form).BasicAuth("client-id", secret.New("private-secret"))); err != nil {
		t.Fatal(err)
	}
	file := []byte("file bytes")
	if _, err := c.Do(t.Context(), c.Post("upload").Multipart(httpclient.Field("title", "Q3 \"report\""), httpclient.File("document", "report.pdf", "application/pdf", file))); err != nil {
		t.Fatal(err)
	}
	file[0] = 'X'
	records := fake.Requests()
	if records[0].Headers().Get("Content-Type") != "application/x-www-form-urlencoded" || string(records[0].Body()) != form.Encode() {
		t.Fatal("form body changed", string(records[0].Body()))
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
	request.Header = records[0].Headers()
	if user, password, ok := request.BasicAuth(); !ok || user != "client-id" || password != "private-secret" {
		t.Fatal("basic credentials changed")
	}
	media, parameters, err := mime.ParseMediaType(records[1].Headers().Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		t.Fatal("multipart content type missing", err)
	}
	reader := multipart.NewReader(bytes.NewReader(records[1].Body()), parameters["boundary"])
	title, err := reader.NextPart()
	if err != nil || title.FormName() != "title" {
		t.Fatal("multipart field missing", err)
	}
	if value, _ := io.ReadAll(title); string(value) != `Q3 "report"` {
		t.Fatal("multipart field changed", string(value))
	}
	document, err := reader.NextPart()
	if err != nil || document.FormName() != "document" || document.FileName() != "report.pdf" || document.Header.Get("Content-Type") != "application/pdf" {
		t.Fatal("multipart file part changed", err)
	}
	if value, _ := io.ReadAll(document); string(value) != "file bytes" {
		t.Fatal("multipart file did not own its bytes", string(value))
	}
	for _, request := range []httpclient.Request{
		c.Post("bad").BasicAuth("user:colon", secret.New("x")),
		c.Post("bad").BasicAuth("", secret.New("x")),
		c.Post("bad").Multipart(),
		c.Post("bad").Multipart(httpclient.Field("", "x")),
		c.Post("bad").Multipart(httpclient.File("f", "a\r\nb", "text/plain", nil)),
	} {
		if request.Validate() == nil {
			t.Fatal("invalid helper input accepted")
		}
	}
	config := testConfig()
	config.RequestBytes = 64
	small := newClient(t, config, fake)
	if small.Post("large").Multipart(httpclient.File("f", "big.bin", "", bytes.Repeat([]byte("x"), 128))).Validate() == nil {
		t.Fatal("multipart body exceeded the request bound")
	}
}

func TestDownloadStreamsBeyondResponseLimitAndDoAllKeepsOrder(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 1000)
	fake, err := fakehttp.NewRoutes(
		fakehttp.On(http.MethodGet, "upstream.test/v1/file", fakehttp.Respond(200, http.Header{"Content-Type": {"application/octet-stream"}}, payload), fakehttp.Respond(404, nil, []byte("missing"))),
		fakehttp.On(http.MethodGet, "upstream.test/v1/items/*", fakehttp.Respond(200, nil, []byte("a")), fakehttp.Respond(200, nil, []byte("b")), fakehttp.Respond(200, nil, []byte("c"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.ResponseBytes = 100
	c := newClient(t, config, fake)
	var sink bytes.Buffer
	result, err := c.Download(t.Context(), c.Get("file"), &sink, int64(len(payload)))
	if err != nil || result.Status != 200 || result.Bytes != int64(len(payload)) || !bytes.Equal(sink.Bytes(), payload) {
		t.Fatal("download did not stream beyond the buffered response limit", err)
	}
	sink.Reset()
	var failure *httpclient.Error
	if _, err := c.Download(t.Context(), c.Get("file"), &sink, int64(len(payload))); !errors.As(err, &failure) || failure.Kind() != httpclient.StatusFailed || sink.Len() != 0 {
		t.Fatal("unsuccessful download wrote a body", err)
	}
	if _, err := c.Do(t.Context(), c.Get("unknown")); !errors.Is(err, fakehttp.Unmatched) {
		t.Fatal("unmatched route answered", err)
	}
	requests := []httpclient.Request{c.Get("items/1"), c.Get("items/2"), c.Get("items/3")}
	outcomes, err := c.DoAll(t.Context(), requests, 2)
	if err != nil || len(outcomes) != 3 {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			t.Fatal(outcome.Err)
		}
		text, _ := outcome.Response.Text()
		seen[text] = true
	}
	if len(seen) != 3 {
		t.Fatal("batch outcomes lost responses", seen)
	}
	fake.AssertSentCount(t, 3, func(r fakehttp.Request) bool { return strings.Contains(r.URL(), "/items/") })
	fake.AssertNotSent(t, func(r fakehttp.Request) bool { return r.Method() == http.MethodPost })
	if _, err := c.DoAll(t.Context(), requests, config.Concurrency+1); err == nil {
		t.Fatal("batch concurrency exceeded client capacity")
	}
}

func TestExhaustedClientCapacityIsATypedOverload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	config := testConfig()
	config.Concurrency = 1
	c := newClient(t, config, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-release
		return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
	}))
	held := make(chan error, 1)
	go func() { _, err := c.Do(context.Background(), c.Get("held")); held <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Do(ctx, c.Get("queued"))
	close(release)
	var failure *httpclient.Error
	if !errors.As(err, &failure) || failure.Kind() != httpclient.Overloaded || !errors.Is(err, fault.Overloaded) {
		t.Fatal("capacity exhaustion was not a typed overload", err)
	}
	if err := <-held; err != nil {
		t.Fatal(err)
	}
}

func TestModuleSnapshotsPolicySlicesAtRegistration(t *testing.T) {
	clientKey := foundation.NewKey[*httpclient.Client]("test.http.snapshot")
	config := testConfig()
	config.BaseURL = ""
	config.Destination = httpclient.PublicDestinations()
	config.Destination.Hosts = []string{"api.example.test"}
	module := httpclient.Module("test.http.snapshot", clientKey, config, nil, nil)
	config.Destination.Hosts[0] = "evil.test"
	app, err := foundry.New().Register(module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), clientKey)
	if err != nil {
		t.Fatal(err)
	}
	if client.Get("https://evil.test/").Validate() == nil || client.Get("https://api.example.test/").Validate() != nil {
		t.Fatal("destination policy changed after module registration")
	}
}
