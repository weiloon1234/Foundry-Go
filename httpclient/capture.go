package httpclient

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
)

// facts hands capture the immutable pieces of a request. Headers are never
// mutated after construction (builders clone them) and Bytes data is a
// private copy, so capture may keep references without copying.
func (r Request) facts() httpcapture.RequestFacts {
	return httpcapture.RequestFacts{Method: r.method, URL: r.url, Headers: r.headers, Data: r.body.data, Stream: r.body.open != nil, Length: r.body.length, Declined: r.withoutBodies}
}

// attemptStage records how far an attempt got, so one function classifies
// its outcome instead of a flag at each of attempt's return sites.
type attemptStage uint8

const (
	// stageRequestBody opens and wraps the request body.
	stageRequestBody attemptStage = iota
	// stageTransport builds the native request, round-trips it and copies
	// the response headers.
	stageTransport
	// stageTooLarge: Content-Length is above the limit; the body is never read.
	stageTooLarge
	// stageResponse validates the response and drains a retried body.
	stageResponse
	// stageConsume: the Do, Stream or Download consumer owns the body.
	stageConsume
)

// classify maps an attempt's final state to its stored result. It runs after
// both bodies are closed, so their failure state is final.
func classify(stage attemptStage, err error, parent, op context.Context, denied, timedOut bool, request, response *ownedstream.Body) httpcapture.Result {
	switch {
	case err == nil:
		return httpcapture.ResultCompleted
	case parent.Err() != nil:
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return httpcapture.ResultTimedOut
		}
		return httpcapture.ResultCanceled
	case errors.Is(op.Err(), context.DeadlineExceeded) || timedOut:
		return httpcapture.ResultTimedOut
	case request.Err() != nil || stage == stageRequestBody:
		return httpcapture.ResultRequestBodyFailed
	case stage == stageTransport && denied:
		return httpcapture.ResultDestinationDenied
	case stage == stageTransport:
		return httpcapture.ResultTransportFailed
	case stage == stageTooLarge || response.Exceeded():
		return httpcapture.ResultResponseTooLarge
	case response.Err() != nil || stage == stageResponse:
		return httpcapture.ResultResponseBodyFailed
	case statusOnly(err):
		return httpcapture.ResultCompleted
	default:
		return httpcapture.ResultConsumerFailed
	}
}

// statusOnly reports a consumer that rejected the status, such as Download's
// StatusFailed for a non-2xx response: the exchange itself completed. The
// error may wrap application errors, so the walk is bounded and isolated.
func statusOnly(err error) bool {
	status := false
	complete := callback.ErrorWalk(err, func(current error) bool {
		if failed, ok := errorgraph.AsShallow[*Error](current); ok && failed.kind == StatusFailed {
			status = true
		}
		return !status
	})
	return complete && status
}
