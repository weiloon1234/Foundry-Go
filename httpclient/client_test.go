package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
	fakehttp "github.com/weiloon1234/Foundry-Go/testkit/httpclient"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testConfig() httpclient.Config {
	config := httpclient.DefaultConfig("test.upstream")
	config.BaseURL = "https://upstream.test/v1"
	config.Retry.InitialBackoff = 0
	config.Retry.MaxBackoff = 0
	return config
}
func newClient(t *testing.T, config httpclient.Config, transport http.RoundTripper) *httpclient.Client {
	t.Helper()
	c, err := httpclient.New(config, transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return c
}
func newFake(t *testing.T, outcomes ...fakehttp.Outcome) *fakehttp.Fake {
	t.Helper()
	f, err := fakehttp.New(outcomes...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNamedRequestBuildersOwnHeadersURLAndBody(t *testing.T) {
	config := testConfig()
	config.Headers = http.Header{"Accept": {"application/json"}, "X-Default": {"original"}}
	fake := newFake(t, fakehttp.Respond(201, http.Header{"X-Reply": {"original"}}, []byte("ok")), fakehttp.Respond(200, nil, nil))
	c := newClient(t, config, fake)
	config.Headers.Set("X-Default", "mutated")
	data := []byte("payload")
	base := c.Post("users/a%2Fb?existing=yes").WithBody(httpclient.Bytes(data))
	data[0] = 'X'
	request := base.QueryPair("q", "private query").Header("X-Request", "private header").Bearer(secret.New("private-token"))
	response, err := c.Do(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status() != 201 || response.Attempts() != 1 || response.EnsureSuccess() != nil {
		t.Fatal(response)
	}
	response.Bytes()[0] = 'X'
	response.Headers().Set("X-Reply", "changed")
	text, err := response.Text()
	if err != nil || text != "ok" || response.Headers().Get("X-Reply") != "original" {
		t.Fatal(text, err)
	}
	if _, err := c.Do(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	records := fake.Requests()
	if records[0].URL() != "https://upstream.test/v1/users/a%2Fb?existing=yes&q=private+query" || string(records[0].Body()) != "payload" || records[0].Headers().Get("Authorization") != "Bearer private-token" || records[0].Headers().Get("X-Default") != "original" {
		t.Fatal("request values or ownership changed")
	}
	if records[1].Headers().Get("Authorization") != "" || strings.Contains(records[1].URL(), "private") {
		t.Fatal("builder changed original request")
	}
	for _, value := range []any{config, c, request, response, records[0], fake, httpclient.Bytes([]byte("private-body"))} {
		rendered := fmt.Sprintf("%v %+v %#v", value, value, value)
		for _, s := range []string{"private", "payload", "upstream.test", "application/json"} {
			if strings.Contains(rendered, s) {
				t.Fatal("HTTP diagnostics exposed request values")
			}
		}
	}
	if snapshot := c.Snapshot(); snapshot.Requests != 2 || snapshot.Attempts != 2 || snapshot.Failures != 0 {
		t.Fatal(snapshot)
	}
}

func TestDefaultRetriesOnlySafeReplayableOperations(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			fake := newFake(t, fakehttp.Respond(503, nil, nil), fakehttp.Respond(500, nil, nil), fakehttp.Respond(200, nil, nil))
			c := newClient(t, testConfig(), fake)
			response, err := c.Do(t.Context(), c.Request(method, "operation").WithBody(httpclient.Bytes([]byte("request"))))
			if err != nil {
				t.Fatal(err)
			}
			attempts, status := 1, 503
			if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
				attempts, status = 3, 200
			}
			if fake.Sent() != attempts || response.Status() != status || response.Attempts() != attempts {
				t.Fatal(fake.Sent(), response)
			}
		})
	}
	fake := newFake(t, fakehttp.Fail(errors.New("private transport detail")), fakehttp.Respond(200, nil, nil))
	c := newClient(t, testConfig(), fake)
	if response, err := c.Do(t.Context(), c.Get("retry")); err != nil || response.Attempts() != 2 {
		t.Fatal(response, err)
	}
	fake = newFake(t, fakehttp.Respond(429, nil, nil), fakehttp.Respond(200, nil, nil))
	c = newClient(t, testConfig(), fake)
	request := c.Post("mutation").Header("Idempotency-Key", "stable-operation").WithBody(httpclient.Bytes([]byte("same"))).WithRetry(httpclient.RetryPolicy{Mode: httpclient.IdempotentOperation, Attempts: 2})
	if response, err := c.Do(t.Context(), request); err != nil || response.Attempts() != 2 {
		t.Fatal(response, err)
	}
	for _, request := range fake.Requests() {
		if string(request.Body()) != "same" {
			t.Fatal("replay changed request body")
		}
	}
}

type countBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b *countBody) Close() error { b.closed.Add(1); return nil }
func TestStreamingRequestBodiesRequireExplicitReplayAndCloseEachAttempt(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprint(replay), func(t *testing.T) {
			var opened, closed atomic.Int32
			opener := func(context.Context) (io.ReadCloser, error) {
				if opened.Load() != closed.Load() {
					t.Error("next attempt opened before prior close")
				}
				opened.Add(1)
				return &countBody{Reader: strings.NewReader("body"), closed: &closed}, nil
			}
			body := httpclient.StreamBody(4, opener)
			if replay {
				body = httpclient.ReplayableBody(4, opener)
			}
			fake := newFake(t, fakehttp.Respond(503, nil, nil), fakehttp.Respond(200, nil, nil))
			c := newClient(t, testConfig(), fake)
			response, err := c.Do(t.Context(), c.Get("stream").WithBody(body))
			if err != nil {
				t.Fatal(err)
			}
			want := int32(1)
			if replay {
				want = 2
			}
			if opened.Load() != want || closed.Load() != want || response.Attempts() != int(want) {
				t.Fatal(opened.Load(), closed.Load(), response)
			}
		})
	}
}

func TestInvalidRequestsFailBeforeOpeningBodiesOrTransport(t *testing.T) {
	fake := newFake(t)
	c := newClient(t, testConfig(), fake)
	called := false
	body := httpclient.StreamBody(-1, func(context.Context) (io.ReadCloser, error) {
		called = true
		return io.NopCloser(strings.NewReader("x")), nil
	})
	requests := []httpclient.Request{
		{}, (httpclient.Request{}).Header("X-Test", "x"), (httpclient.Request{}).QueryPair("a", "b"), (httpclient.Request{}).WithBody(body),
		c.Get("https://other.test/").WithBody(body), c.Get("//other.test/").WithBody(body), c.Get("../outside").WithBody(body), c.Get("%2e%2e/outside").WithBody(body),
		c.Get("ok").Header("Authorization", "bad\r\nvalue").WithBody(body), c.Get("ok").Header("Content-Length", "1").WithBody(body), c.Get("ok").Header("Bad Header", "x").WithBody(body),
		c.Get("ok").Bearer(secret.New("bad token")).WithBody(body), c.Request("bad method", "ok").WithBody(body), c.Get("ok#fragment").WithBody(body),
	}
	for _, request := range requests {
		if _, err := c.Do(t.Context(), request); err == nil {
			t.Fatal("invalid request sent")
		}
	}
	other := newClient(t, testConfig(), fake)
	if _, err := other.Do(t.Context(), c.Get("ok")); err == nil {
		t.Fatal("request crossed client policy")
	}
	if called || fake.Sent() != 0 {
		t.Fatal("invalid input reached extension")
	}
	for _, base := range []string{"https://user:password@upstream.test", "file:///tmp/secret", "https://upstream.test/?token=private"} {
		config := testConfig()
		config.BaseURL = base
		if _, err := httpclient.New(config, nil); err == nil {
			t.Fatal("invalid base URL accepted")
		}
	}
}

func TestResponseLimitsStatusAndEscapedStream(t *testing.T) {
	config := testConfig()
	config.ResponseBytes = 4
	fake := newFake(t, fakehttp.Respond(200, nil, []byte("large")))
	c := newClient(t, config, fake)
	if response, err := c.Do(t.Context(), c.Get("large")); err == nil || len(response.Bytes()) != 0 {
		t.Fatal("oversized response returned data", err)
	}
	var closes atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: &countBody{Reader: strings.NewReader("larger"), closed: &closes}}, nil
	})
	c = newClient(t, config, transport)
	if _, err := c.Do(t.Context(), c.Get("unknown")); err == nil {
		t.Fatal("unknown-length body exceeded bound")
	}
	if closes.Load() != 1 {
		t.Fatal("failed response not closed")
	}
	fake = newFake(t, fakehttp.Respond(302, nil, nil), fakehttp.Respond(200, nil, []byte("body")))
	c = newClient(t, config, fake)
	response, err := c.Do(t.Context(), c.Get("redirect"))
	if err != nil || response.Status() != 302 {
		t.Fatal(response, err)
	}
	var status *httpclient.Error
	if !errors.As(response.EnsureSuccess(), &status) || status.Kind() != httpclient.StatusFailed {
		t.Fatal("status classification")
	}
	var escaped *httpclient.StreamResponse
	if err := c.Stream(t.Context(), c.Get("stream"), func(_ context.Context, response *httpclient.StreamResponse) error {
		escaped = response
		buffer := make([]byte, 1)
		_, err := io.ReadFull(response, buffer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := escaped.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("stream escaped lifetime", err)
	}
}

func TestForeignTransportErrorsKeepSafeDiagnosticsAndActualKind(t *testing.T) {
	private := errors.New("secret-query-password-body")
	fake := newFake(t, fakehttp.Fail(private))
	config := testConfig()
	config.Retry = httpclient.NoRetries()
	c := newClient(t, config, fake)
	_, err := c.Do(t.Context(), c.Get("operation"))
	var classified *httpclient.Error
	if !errors.As(err, &classified) || classified.Kind() != httpclient.TransportFailed || !errors.Is(err, private) || classified.Attempt() != 1 {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), private.Error()) {
		t.Fatal("transport diagnostic leaked")
	}
}
