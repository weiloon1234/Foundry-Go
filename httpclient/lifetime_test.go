package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not reach expected phase")
	}
}
func awaitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not return")
		return nil
	}
}

func TestCloseRetainsUncooperativeTransportOwnership(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return &http.Response{StatusCode: 204, Body: http.NoBody, ContentLength: 0}, nil
	})
	config := testConfig()
	config.Concurrency = 1
	c := newClient(t, config, transport)
	result := make(chan error, 1)
	go func() { _, err := c.Do(context.Background(), c.Get("held")); result <- err }()
	await(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := c.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
		t.Fatal("claimed drain before transport exit")
	default:
	}
	if _, err := c.Do(t.Context(), c.Get("next")); !errors.Is(err, fault.Conflict) {
		t.Fatal("shutdown admitted work", err)
	}
	once.Do(func() { close(release) })
	if err := awaitResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	await(t, c.Done())
}

type heldCloseBody struct {
	io.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (b *heldCloseBody) Close() error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}
func TestResponseCloseCompletesBeforeAdmissionIsReleased(t *testing.T) {
	closed, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	body := &heldCloseBody{Reader: strings.NewReader("ok"), entered: closed, release: release}
	config := testConfig()
	config.Concurrency = 1
	c := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, ContentLength: 2}, nil
	}))
	result := make(chan error, 1)
	go func() { _, err := c.Do(context.Background(), c.Get("close")); result <- err }()
	await(t, closed)
	if _, err := c.Do(t.Context(), c.Get("next")); !errors.Is(err, fault.Conflict) {
		t.Fatal("capacity released before actual close", err)
	}
	select {
	case err := <-result:
		t.Fatal("caller abandoned close", err)
	default:
	}
	once.Do(func() { close(release) })
	if err := awaitResult(t, result); err != nil {
		t.Fatal(err)
	}
}

type interruptedUpload struct {
	started, closed, finished chan struct{}
	once                      sync.Once
}

func (b *interruptedUpload) Read([]byte) (int, error) {
	close(b.started)
	<-b.closed
	close(b.finished)
	return 0, io.EOF
}
func (b *interruptedUpload) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

type waitUploadResponse struct{ finished <-chan struct{} }

func (b waitUploadResponse) Read([]byte) (int, error) { return 0, io.EOF }
func (b waitUploadResponse) Close() error             { <-b.finished; return nil }
func TestEarlyResponseCleanupInterruptsOutstandingUploadBeforeWaitingResponse(t *testing.T) {
	upload := &interruptedUpload{started: make(chan struct{}), closed: make(chan struct{}), finished: make(chan struct{})}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		go func() { _, _ = r.Body.Read(make([]byte, 1)) }()
		<-upload.started
		return &http.Response{StatusCode: 204, ContentLength: 0, Body: waitUploadResponse{upload.finished}}, nil
	})
	c := newClient(t, testConfig(), transport)
	result := make(chan error, 1)
	go func() {
		_, err := c.Do(context.Background(), c.Post("early").WithBody(httpclient.StreamBody(-1, func(context.Context) (io.ReadCloser, error) { return upload, nil })))
		result <- err
	}()
	if err := awaitResult(t, result); err != nil {
		t.Fatal(err)
	}
	await(t, upload.finished)
}

func TestStreamContextRetainsDeadlineAndRejectsSelfShutdown(t *testing.T) {
	config := testConfig()
	config.AttemptTimeout = time.Second
	var calls atomic.Int32
	c := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), ContentLength: 2}, nil
	}))
	if err := c.Stream(t.Context(), c.Get("self"), func(ctx context.Context, r *httpclient.StreamResponse) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("attempt deadline lost")
		}
		if err := c.Close(ctx); !errors.Is(err, fault.Cycle) {
			t.Error("self shutdown did not fail before mutation", err)
		}
		_, err := io.ReadAll(r)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(t.Context(), c.Get("still-open")); err != nil || calls.Load() != 2 {
		t.Fatal("self close partially shut down client", err)
	}
}

func TestCallbackFailureAndSwallowedReadFailureCannotReportSuccess(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(map[bool]string{false: "panic", true: "Goexit"}[exit], func(t *testing.T) {
			var closed atomic.Int32
			c := newClient(t, testConfig(), roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: &countBody{Reader: strings.NewReader("ok"), closed: &closed}, ContentLength: 2}, nil
			}))
			err := c.Stream(t.Context(), c.Get("failure"), func(context.Context, *httpclient.StreamResponse) error {
				if exit {
					runtime.Goexit()
				}
				panic("private callback data")
			})
			if err == nil || !errors.Is(err, fault.Panicked) || closed.Load() != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal(err, closed.Load())
			}
		})
	}
	config := testConfig()
	config.ResponseBytes = 1
	c := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("too long")), ContentLength: -1}, nil
	}))
	if err := c.Stream(t.Context(), c.Get("ignored"), func(_ context.Context, r *httpclient.StreamResponse) error { _, _ = io.ReadAll(r); return nil }); err == nil {
		t.Fatal("callback swallowed body bound failure")
	}
}

func TestFailedBodyFactoriesAndTransportResultsStillCloseAcquiredReaders(t *testing.T) {
	var opened, closed, transported atomic.Int32
	c := newClient(t, testConfig(), roundTripFunc(func(*http.Request) (*http.Response, error) {
		transported.Add(1)
		return nil, errors.New("transport must not run")
	}))
	body := httpclient.StreamBody(-1, func(context.Context) (io.ReadCloser, error) {
		opened.Add(1)
		return &countBody{Reader: strings.NewReader("partial"), closed: &closed}, errors.New("private open error")
	})
	if _, err := c.Do(t.Context(), c.Get("open").WithBody(body)); err == nil || opened.Load() != 1 || closed.Load() != 1 || transported.Load() != 0 {
		t.Fatal("failed factory leaked its acquired reader", err)
	}
	closed.Store(0)
	c = newClient(t, testConfig(), roundTripFunc(func(*http.Request) (*http.Response, error) {
		if transported.Load() != closed.Load() {
			t.Error("retry began before prior body closed")
		}
		transported.Add(1)
		return &http.Response{StatusCode: 200, Body: &countBody{Reader: strings.NewReader("partial"), closed: &closed}, ContentLength: 7}, errors.New("private transport error")
	}))
	if _, err := c.Do(t.Context(), c.Get("transport")); err == nil || transported.Load() != 3 || closed.Load() != 3 {
		t.Fatal("failed transport response leaked or skipped bounded retry", err, transported.Load(), closed.Load())
	}
}

type failingCloseBody struct{ io.Reader }

func (failingCloseBody) Close() error { return errors.New("private close error") }

func TestBodyCloseFailurePreventsRetryAndSuccessfulPublication(t *testing.T) {
	for _, status := range []int{200, 503} {
		var attempts atomic.Int32
		c := newClient(t, testConfig(), roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{StatusCode: status, Body: failingCloseBody{strings.NewReader("body")}, ContentLength: 4}, nil
		}))
		response, err := c.Do(t.Context(), c.Get("close"))
		var classified *httpclient.Error
		if !errors.As(err, &classified) || classified.Kind() != httpclient.BodyFailed || attempts.Load() != 1 || response.Status() != 0 || strings.Contains(err.Error(), "private") {
			t.Fatal("failed close retried or published a response", err, attempts.Load())
		}
	}
}
