package http

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func endpointNameRules() validation.Rule[EndpointPatch] {
	name := validation.DefineField("name", func(body EndpointPatch) string { return body.Name })
	return name.Rules(validation.MinLength[string](3).WithMessage("Use at least three characters."))
}

func TestEndpointValidationKeepsSourcesAndStopsInvalidHandlers(t *testing.T) {
	t.Parallel()
	term := validation.DefineField("q", func(query endpointParameters) value.Optional[string] { return query.Term })
	base := patchEndpoint()
	endpoint := base.WithQueryValidation(term.Rules(validation.Optional(validation.MinLength[string](2)))).WithBodyValidation(endpointNameRules())
	var called atomic.Int32
	router, err := NewRouter(endpoint.Handle(func(_ context.Context, input endpointRequest) (EndpointReply, error) {
		called.Add(1)
		return EndpointReply{Name: input.Body.Name}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PATCH", "/items/"+endpointUserID+"?q=x", strings.NewReader(`{"name":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	failure := decodeFailure(t, response)
	if response.Code != 422 || called.Load() != 0 || len(failure.Issues) != 2 || failure.Issues[0].Path != "/query/q" || failure.Issues[1].Path != "/body/name" || failure.Issues[1].Message != "Use at least three characters." || failure.Issues[1].Code != contract.IssueCode("foundry.min_length") {
		t.Fatalf("validation result: %d %+v", response.Code, failure)
	}
	request = httptest.NewRequest("PATCH", "/items/"+endpointUserID+"?q=ok", strings.NewReader(`{"name":"valid"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 201 || called.Load() != 1 {
		t.Fatal("valid request did not reach handler")
	}
	before, err := base.Description()
	if err != nil || before.Validation != nil {
		t.Fatal("adding validation changed base endpoint")
	}
	first := router.Endpoints()
	if len(first) != 1 || first[0].Validation == nil {
		t.Fatal("validation metadata missing")
	}
	first[0].Validation.Children = nil
	if len(router.Endpoints()[0].Validation.Children) == 0 {
		t.Fatal("validation metadata was shared")
	}
}

func TestEndpointValidationDeclarationAndResourceFailures(t *testing.T) {
	t.Parallel()
	var zero validation.Rule[EndpointPatch]
	if patchEndpoint().WithBodyValidation(zero).Validate() == nil || patchEndpoint().WithBodyValidation().Validate() == nil {
		t.Fatal("undefined validation accepted")
	}
	limits := DefaultEndpointLimits()
	limits.Validation.Checks = 1
	endpoint := patchEndpoint().WithBodyValidation(endpointNameRules()).WithLimits(limits)
	router, err := NewRouter(endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("incomplete validation reached handler")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"valid"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 400 || len(decodeFailure(t, response).Issues) != 0 {
		t.Fatal("resource failure became rule rejection")
	}
	limits = DefaultEndpointLimits()
	limits.Validation.Issues = 1
	endpoint = patchEndpoint().WithBodyValidation(endpointNameRules(), endpointNameRules()).WithLimits(limits)
	router, err = NewRouter(endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("invalid input reached handler")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	failure := decodeFailure(t, response)
	if response.Code != 422 || len(failure.Issues) != 1 || !failure.IssuesTruncated {
		t.Fatal("truncated issues were not declared")
	}
}

func TestEndpointValidationFailureOwnership(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "returned", "cancel-panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rule := validation.Custom(validation.Spec{ID: "app.failing", Message: "Approved public text."}, func(context.Context, EndpointPatch) (bool, error) {
				switch mode {
				case "returned":
					return false, errors.New("private infrastructure text")
				case "goexit":
					runtime.Goexit()
				case "cancel-panic":
					cancel()
				}
				panic("private panic text")
			})
			router, err := NewRouter(patchEndpoint().WithBodyValidation(rule).Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
				t.Error("failed rule reached handler")
				return EndpointReply{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequestWithContext(ctx, "PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"valid"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 500 || len(decodeFailure(t, response).Issues) != 0 || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("unsafe rule failure: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEndpointValidationCancellationWaitsForRule(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	rule := validation.Custom(validation.Spec{ID: "app.waiting", Message: "Wait."}, func(context.Context, EndpointPatch) (bool, error) { close(entered); <-release; return true, nil })
	router, err := NewRouter(patchEndpoint().WithBodyValidation(rule).Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("canceled rule reached handler")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, "PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"valid"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	go func() { defer close(done); router.ServeHTTP(response, request) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("rule was abandoned")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rule did not finish")
	}
	// Validation runs after decoding: an ended request context is the server
	// phase (503), never the slow-client 408.
	if response.Code != 503 {
		t.Fatal("canceled validation lost its unavailable classification", response.Code)
	}
}

func TestWrappedValidationErrorsUseSharedResponseContract(t *testing.T) {
	t.Parallel()
	rejected := validation.NonBlank[string]().Check(t.Context(), " ", validation.DefaultLimits())
	for _, internal := range []bool{false, true} {
		var err error = fmt.Errorf("private wrapping detail: %w", rejected)
		if internal {
			err = InternalError.WithCause(err)
		}
		response := httptest.NewRecorder()
		if failure := WriteError(response, httptest.NewRequest("GET", "/", nil), err); failure != nil {
			t.Fatal(failure)
		}
		failure := decodeFailure(t, response)
		if strings.Contains(response.Body.String(), "private") {
			t.Fatal("wrapped detail leaked")
		}
		if internal {
			if response.Code != 500 || len(failure.Issues) != 0 {
				t.Fatal("internal cause exposed validation details")
			}
		} else if response.Code != 422 || len(failure.Issues) != 1 || failure.Issues[0].Message == "" {
			t.Fatal("wrapped validation lost public details")
		}
	}
}
