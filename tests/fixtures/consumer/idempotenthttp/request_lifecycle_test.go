package idempotenthttp

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestIdempotentRequestLifecycleAndReplayResponse(t *testing.T) {
	app := startApp(t, pgtest.Isolate(t), Hooks{}, nil)
	store, err := app.app.Resources().Idempotency()
	if err != nil {
		t.Fatal(err)
	}
	type input = http.Input[http.NoPath, http.NoQuery, Submission]
	var stages []string
	endpoint := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "lifecycle", Method: http.POST, Access: http.Public}, http.StaticPath("/lifecycle")), http.EmptyQuery(), http.JSONBody(SubmissionJSON()), http.JSONResponse(201, SubmissionJSON()))
	fields := SubmissionValidationFields()
	endpoint = http.ValidateBody(endpoint, validation.All(
		validation.Rules(fields.Memo, validation.Absent[value.Nullable[string]]()),
		validation.Rules(fields.Name, validation.Custom(validation.Spec{ID: "lifecycle.name", Message: "Name is required."}, func(_ context.Context, name string) (bool, error) {
			stages = append(stages, "validate")
			return name != "", nil
		})),
	))
	endpoint = http.PrepareEndpoint(endpoint, func(_ context.Context, in input) (http.NoQuery, Submission, error) {
		stages = append(stages, "prepare")
		in.Body.Name = strings.TrimSpace(in.Body.Name)
		in.Body.Memo = value.Optional[value.Nullable[string]]{}
		return in.Query, in.Body, nil
	})
	endpoint = http.AuthorizeEndpoint(endpoint, func(_ context.Context, in input) error {
		stages = append(stages, "authorize")
		if in.Body.Name == "deny" {
			return http.Forbidden
		}
		return nil
	})
	operation := http.IdempotentHeaders(http.Idempotent(endpoint, store, idempotency.Definition{ID: "lifecycle", Version: 1}), func(context.Context, Submission) ([]http.ResponseHeader, error) {
		stages = append(stages, "headers")
		return []http.ResponseHeader{{Name: "Location", Value: "/result"}}, nil
	})
	registration := operation.Handle(func(context.Context, input) (idempotency.Scope, error) {
		stages = append(stages, "scope")
		return idempotency.NewScope("lifecycle", "caller")
	}, func(_ context.Context, tx *database.Tx, in input) (Submission, error) {
		stages = append(stages, "handler")
		if tx == nil || in.Body.Name != "Jane" {
			t.Error("handler lost transaction or prepared input")
		}
		return in.Body, nil
	})
	router, err := http.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	metadata := router.Endpoints()
	if len(metadata) != 1 || metadata[0].Status != 201 || metadata[0].Idempotency == nil || metadata[0].Response == nil || !metadata[0].Preparation || metadata[0].Validation == nil {
		t.Fatal("idempotent metadata changed", metadata)
	}
	var first *httptest.ResponseRecorder
	for _, tc := range []struct {
		body   string
		status int
		order  []string
	}{
		{`{"name":"Jane","memo":"original"}`, 422, nil},
		{`{"name":123}`, 400, nil},
		{`{"name":"deny"}`, 403, []string{"prepare", "authorize"}},
		{`{"name":" "}`, 422, []string{"prepare", "authorize", "validate"}},
		{`{"name":"  Jane  "}`, 201, []string{"prepare", "authorize", "validate", "scope", "handler", "headers"}},
		{`{"name":"  Jane  "}`, 201, []string{"prepare", "authorize", "validate", "scope"}},
	} {
		stages = nil
		req := httptest.NewRequest("POST", "/lifecycle", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "lifecycle-request-0001")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.status || !slices.Equal(stages, tc.order) {
			t.Fatalf("%s: status %d, stages %v, body %s", tc.body, response.Code, stages, response.Body.String())
		}
		if tc.status == 201 {
			if response.Header().Get("Location") != "/result" || response.Header().Get("Content-Type") != "application/json" || !strings.Contains(response.Body.String(), `"name":"Jane"`) {
				t.Fatal("idempotent response changed", response.Header(), response.Body.String())
			}
			if first == nil {
				first = response
			} else if first.Body.String() != response.Body.String() || !slices.Equal(first.Header().Values("Location"), response.Header().Values("Location")) || first.Header().Get("Content-Length") != response.Header().Get("Content-Length") {
				t.Fatal("replay response differs")
			}
		}
	}
}
