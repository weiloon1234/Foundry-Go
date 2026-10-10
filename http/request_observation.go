package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

type requestObservation struct {
	started  time.Time
	security *SecurityRequestEvent
	// route is published by the matched route's handler goroutine and read
	// after the handler returns; the pointer is immutable per-route state.
	route        atomic.Pointer[matchedRoute]
	diagnosticMu sync.Mutex
	diagnostic   fault.Diagnostic
	span         *observability.Span
	outcome      observability.Outcome
	contextErr   error
	returned     bool
}

func beginRequestObservation(request *stdhttp.Request, trust bool) (context.Context, *requestObservation) {
	ctx := tracing.WithoutContext(request.Context())
	if trust {
		parents := request.Header.Values(tracing.ParentHeader)
		if len(parents) == 1 {
			// Bound the concatenation before allocating vendor metadata. Invalid
			// state is discarded independently of an otherwise valid parent.
			states := request.Header.Values(tracing.StateHeader)
			size := max(0, len(states)-1)
			for _, state := range states {
				size += len(state)
			}
			state := ""
			if size <= tracing.MaxStateBytes {
				state = strings.Join(states, ",")
			}
			if parent, err := tracing.Parse(parents[0], state); err == nil {
				ctx, _ = tracing.WithContext(ctx, parent)
			}
		}
	}
	recorder := observability.FromContext(ctx)
	if recorder == nil {
		return ctx, nil
	}
	work, span, err := recorder.Start(ctx, observability.Operation{Kind: observability.HTTP, Name: "request"})
	if err != nil {
		return ctx, nil
	}
	return work, &requestObservation{span: span, returned: true}
}

func (o *requestObservation) result(response *observedResponse) observability.Result {
	status := response.status
	if status == 0 && !response.hijacked && o.returned {
		status = stdhttp.StatusOK
	}
	// Native net/http permits nonstandard 600–999 codes. Preserve the wire
	// behavior while recording a failed response without an invalid label.
	if status > 599 {
		status = 0
		o.outcome = observability.Failed
	}
	if o.outcome == "" {
		switch {
		case !o.returned:
			o.outcome = observability.Panicked
		// A successful hijack transfers cancellation to its explicit owner.
		// The old HTTP request deadline must not label a healthy long-lived
		// WebSocket connection as a failed ordinary response.
		case o.contextErr != nil && !response.hijacked:
			o.outcome = observability.OutcomeFor(o.contextErr)
		case response.err != nil:
			o.outcome = observability.OutcomeFor(response.err)
		}
	}
	result, _ := (observability.Result{Outcome: o.outcome, Status: status}).Normalized(observability.HTTP)
	return result
}
