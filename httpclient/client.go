package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	sharedtransport "github.com/weiloon1234/Foundry-Go/internal/httptransport"
	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

// Client owns native connection pooling and bounded operation lifetimes. A
// supplied RoundTripper is borrowed and must honor request cancellation, close
// bodies and avoid implementing an additional retry/redirect policy.
type Client struct {
	config                       Config
	base                         *url.URL
	transport                    http.RoundTripper
	owned                        *http.Transport
	operations                   *workscope.Group
	requests, attempts, failures atomic.Uint64
}

func New(config Config, transport http.RoundTripper) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if transport != nil && (ownedstream.Nil(transport) || config.Destination.Mode == RestrictedDestinations) {
		return nil, invalid()
	}
	headers, err := copyHeaders(config.Headers, config.HeaderBytes, true)
	if err != nil {
		return nil, err
	}
	config.Headers = headers
	config.Destination = config.Destination.snapshot()
	var base *url.URL
	if config.BaseURL != "" {
		base, err = baseURL(config.BaseURL)
		if err != nil {
			return nil, err
		}
	}
	operations, err := workscope.New(config.Concurrency, config.Timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{config: config, base: base, transport: transport, operations: operations}
	if transport == nil {
		c.owned = sharedtransport.New(sharedtransport.Config{ConnectTimeout: config.ConnectTimeout, RequestTimeout: config.AttemptTimeout, MaxIdleConnections: config.Concurrency, MaxIdlePerHost: config.Concurrency, MaxResponseHeaderBytes: int64(config.HeaderBytes)})
		if config.Destination.Mode == RestrictedDestinations {
			c.owned.Proxy = nil
			dialer := newDestinationDialer(config.Destination, config.ConnectTimeout)
			c.owned.DialContext = dialer.dial
		}
		c.transport = c.owned
	}
	return c, nil
}

func (c *Client) Name() Name {
	if c == nil {
		return ""
	}
	return c.config.Name
}
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.operations == nil {
		return invalid()
	}
	if err := c.operations.Close(ctx); err != nil {
		return err
	}
	if c.owned != nil {
		c.owned.CloseIdleConnections()
	}
	return nil
}
func (c *Client) Done() <-chan struct{} {
	if c == nil {
		return (*workscope.Group)(nil).Done()
	}
	return c.operations.Done()
}
func (*Client) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("outbound HTTP client")) }

// Snapshot contains only bounded operation counts; no request values are retained.
type Snapshot struct{ Requests, Attempts, Failures uint64 }

func (c *Client) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return Snapshot{Requests: c.requests.Load(), Attempts: c.attempts.Load(), Failures: c.failures.Load()}
}

func (c *Client) Do(ctx context.Context, request Request) (Response, error) {
	var result Response
	err := c.execute(ctx, request, func(op context.Context, info responseInfo, body *ownedstream.Body) error {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		if err := op.Err(); err != nil {
			return err
		}
		result = Response{info: info, data: data}
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	return result, nil
}

// Stream lends one response body to consume. It always closes the stream, and
// retains admission until consume and actual body closure finish. Retained readers
// reject use after return. Once consume starts, the request is never retried.
func (c *Client) Stream(ctx context.Context, request Request, consume func(context.Context, *StreamResponse) error) error {
	if consume == nil {
		return invalid()
	}
	return c.execute(ctx, request, func(op context.Context, info responseInfo, body *ownedstream.Body) error {
		return consume(op, &StreamResponse{info: info, body: body})
	})
}

type responseConsumer func(context.Context, responseInfo, *ownedstream.Body) error

func (c *Client) execute(ctx context.Context, request Request, consume responseConsumer) error {
	if c == nil || ctx == nil || c.operations == nil {
		return invalid()
	}
	if err := request.Validate(); err != nil {
		return failure(InvalidRequest, c.Name(), request.method, 0, 0, err)
	}
	if request.client != c {
		return invalid()
	}
	lease, err := c.operations.Begin(ctx)
	if err != nil {
		return err
	}
	defer lease.Release()
	c.requests.Add(1)
	var operationErr error
	invokeErr := callback.Isolated("outbound HTTP operation", func() error {
		operationErr = observability.Observe(lease.Context(), observability.Operation{Kind: observability.OutboundHTTP, Name: observability.Name(c.Name())}, func(ctx context.Context) error { return c.runOperation(ctx, request, consume) })
		return nil
	})
	if invokeErr != nil {
		c.failures.Add(1)
		return failure(CallbackFailed, c.Name(), request.method, 0, 0, invokeErr)
	}
	if operationErr != nil {
		c.failures.Add(1)
	}
	return operationErr
}

func (c *Client) runOperation(op context.Context, request Request, consume responseConsumer) error {
	attempts := request.retry.attempts(request.method, request.body)
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := op.Err(); err != nil {
			return err
		}
		retry, err := c.attempt(op, request, attempt, attempt < attempts, consume)
		if !retry {
			return canceled(op, err)
		}
		if err := waitRetry(op, request.retry.delay(attempt)); err != nil {
			return err
		}
	}
	return invalid()
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func (c *Client) attempt(parent context.Context, request Request, number int, canRetry bool, consume responseConsumer) (retry bool, err error) {
	deadline, cancel := context.WithTimeout(parent, c.config.AttemptTimeout)
	op, unlink := contextlink.Link(deadline, parent)
	defer cancel()
	defer unlink()
	var requestBody, responseBody *ownedstream.Body
	defer func() {
		var cleanup error
		if requestBody != nil {
			cleanup = errors.Join(cleanup, requestBody.Close(), requestBody.Err())
		}
		if responseBody != nil {
			cleanup = errors.Join(cleanup, responseBody.Close(), responseBody.Err())
		}
		if cleanup != nil {
			retry = false
			err = failure(BodyFailed, c.Name(), request.method, number, 0, errors.Join(err, cleanup))
		}
		if parent.Err() != nil {
			retry = false
			err = parent.Err()
		}
	}()
	var source io.ReadCloser
	var openErr error
	invokeErr := callback.Isolated("HTTP request body open", func() error { source, openErr = request.body.reader(op); return nil })
	if !ownedstream.Nil(source) {
		requestBody, err = ownedstream.New(op, source, c.config.RequestBytes, request.body.length)
		if err != nil {
			return false, err
		}
	}
	if invokeErr != nil || openErr != nil || requestBody == nil {
		return false, failure(BodyFailed, c.Name(), request.method, number, 0, errors.Join(invokeErr, openErr))
	}
	if err := op.Err(); err != nil {
		return false, err
	}
	native, err := http.NewRequestWithContext(op, request.method, request.url.String(), requestBody)
	if err != nil {
		return false, failure(InvalidRequest, c.Name(), request.method, number, 0, err)
	}
	native.Header = request.headers.Clone()
	if c.config.PropagateTrace {
		native.Header.Del(tracing.ParentHeader)
		native.Header.Del(tracing.StateHeader)
		if trace := tracing.FromContext(op); !trace.IsZero() {
			native.Header.Set(tracing.ParentHeader, trace.TraceParent())
			if trace.TraceState() != "" {
				native.Header.Set(tracing.StateHeader, trace.TraceState())
			}
		}
		native.Header, err = copyHeaders(native.Header, c.config.HeaderBytes, true)
		if err != nil {
			return false, failure(InvalidRequest, c.Name(), request.method, number, 0, err)
		}
	}
	native.ContentLength = request.body.length
	// Keep even an empty body behind the owned reader, with no GetBody. This
	// prevents net/http from replaying an Idempotency-Key POST behind this policy.
	native.GetBody = nil
	c.attempts.Add(1)
	var response *http.Response
	var transportErr error
	invokeErr = callback.Isolated("HTTP transport", func() error { response, transportErr = c.transport.RoundTrip(native); return nil })
	if response != nil && !ownedstream.Nil(response.Body) {
		expected := response.ContentLength
		if request.method == http.MethodHead || response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusNotModified {
			expected = -1
		}
		// Acquire the returned body even if its metadata is malformed, so the
		// rejection path still owns and closes it.
		if expected < -1 {
			expected = -1
		}
		responseBody, err = ownedstream.New(op, response.Body, c.config.ResponseBytes, expected)
		if err != nil {
			return false, err
		}
	}
	if invokeErr != nil {
		return false, failure(CallbackFailed, c.Name(), request.method, number, 0, invokeErr)
	}
	if parent.Err() != nil {
		return false, parent.Err()
	}
	if transportErr != nil || op.Err() != nil {
		return canRetry && !errorgraph.Is(transportErr, DestinationDenied), failure(TransportFailed, c.Name(), request.method, number, 0, canceled(op, transportErr))
	}
	if response == nil || responseBody == nil || response.StatusCode < 100 || response.StatusCode > 599 {
		return false, failure(TransportFailed, c.Name(), request.method, number, 0, invalid())
	}
	if response.ContentLength < -1 || request.method != http.MethodHead && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified && response.ContentLength > c.config.ResponseBytes {
		return false, failure(BodyFailed, c.Name(), request.method, number, response.StatusCode, invalid())
	}
	headers, err := copyHeaders(response.Header, c.config.HeaderBytes, false)
	if err != nil {
		return false, failure(TransportFailed, c.Name(), request.method, number, response.StatusCode, err)
	}
	if canRetry && retryStatus(response.StatusCode) {
		// A bounded drain allows connection reuse for small rejection bodies.
		// Larger bodies are closed before retry; they are never buffered in full.
		_, drainErr := io.CopyN(io.Discard, responseBody, min(c.config.ResponseBytes, 64<<10))
		if drainErr != nil && drainErr != io.EOF {
			return false, failure(BodyFailed, c.Name(), request.method, number, response.StatusCode, drainErr)
		}
		return true, nil
	}
	info := responseInfo{name: c.Name(), method: request.method, status: response.StatusCode, attempt: number, headers: headers, maximum: c.config.ResponseBytes}
	var consumeErr error
	if err := callback.Isolated("HTTP response consumer", func() error { consumeErr = consume(op, info, responseBody); return nil }); err != nil {
		return false, failure(CallbackFailed, c.Name(), request.method, number, response.StatusCode, err)
	}
	if consumeErr != nil {
		return false, canceled(op, failure(BodyFailed, c.Name(), request.method, number, response.StatusCode, consumeErr))
	}
	return false, canceled(op, nil)
}
