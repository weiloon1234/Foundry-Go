package httpclient_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestOutboundTracePropagationIsExplicitAndRequestOwned(t *testing.T) {
	for _, propagate := range []bool{false, true} {
		config := testConfig()
		config.PropagateTrace = propagate
		recorder, err := observability.New(observability.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		parent, err := tracing.New(true)
		if err != nil {
			t.Fatal(err)
		}
		parent, err = parent.WithState("vendor=private")
		if err != nil {
			t.Fatal(err)
		}
		ctx, err := tracing.WithContext(observability.WithContext(t.Context(), recorder), parent)
		if err != nil {
			t.Fatal(err)
		}
		var sent []string
		client := newClient(t, config, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			defer request.Body.Close()
			sent = append(sent, request.Header.Get(tracing.ParentHeader))
			if propagate {
				trace, err := tracing.Parse(sent[len(sent)-1], request.Header.Get(tracing.StateHeader))
				if err != nil || trace.TraceID() != parent.TraceID() || trace.SpanID() == parent.SpanID() || trace.TraceState() != parent.TraceState() {
					t.Error("outbound child context lost", err)
				}
			} else if sent[len(sent)-1] != "" || request.Header.Get(tracing.StateHeader) != "" {
				t.Error("default client exported correlation")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), ContentLength: 2}, nil
		}))
		request := client.Get("/resource")
		for range 2 {
			if _, err := client.Do(ctx, request); err != nil {
				t.Fatal(err)
			}
		}
		if propagate && sent[0] == sent[1] {
			t.Fatal("reused immutable request reused completed span")
		}
		if snapshot := recorder.Snapshot(); snapshot.Completed != 2 || snapshot.Active != 0 || snapshot.Recent[0].Operation.Kind != observability.OutboundHTTP {
			t.Fatal("outbound observations missing", snapshot)
		}
		if err := recorder.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPropagationHonorsTotalHeaderBudget(t *testing.T) {
	config := testConfig()
	config.PropagateTrace = true
	config.HeaderBytes = 32
	client := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("oversized propagation reached transport")
		return nil, nil
	}))
	parent, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(ctx, client.Get("/resource")); err == nil {
		t.Fatal("trace headers bypassed named client budget")
	}
}
