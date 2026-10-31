package http_test

import (
	"context"
	"io"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type pipelineForm struct {
	Name   string
	Secret value.Optional[string]
}
type pipelineInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, pipelineForm]

type pipelineReader struct {
	io.Reader
	first func()
	close func()
}

func (r *pipelineReader) Read(p []byte) (int, error) {
	if r.first != nil {
		r.first()
		r.first = nil
	}
	return r.Reader.Read(p)
}
func (r *pipelineReader) Close() error {
	if r.close != nil {
		r.close()
	}
	return nil
}

type pipelineWriter struct {
	*httptest.ResponseRecorder
	stages *[]string
}

func (w pipelineWriter) WriteHeader(status int) {
	*w.stages = append(*w.stages, "publish")
	w.ResponseRecorder.WriteHeader(status)
}

func pipelineEndpoint[R any](response foundryhttp.Response[R], stages *[]string, access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, pipelineForm, R] {
	body := foundryhttp.FormBody(foundryhttp.DefineQuery(
		foundryhttp.QueryParam("name", foundryhttp.StringQuery[string](), func(b *pipelineForm) *string { return &b.Name }),
		foundryhttp.OptionalQueryParam("secret", foundryhttp.StringQuery[string](), func(b *pipelineForm) *value.Optional[string] { return &b.Secret }),
	))
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "pipeline", Method: foundryhttp.POST, Access: access}, foundryhttp.StaticPath("/pipeline")), foundryhttp.EmptyQuery(), body, response)
	endpoint = foundryhttp.ValidateBody(endpoint, validation.All(
		validation.Rules(validation.DefineField("secret", func(b pipelineForm) value.Optional[string] { return b.Secret }), validation.Absent[string]()),
		validation.Rules(validation.DefineField("name", func(b pipelineForm) string { return b.Name }), validation.Custom(validation.Spec{ID: "pipeline.name", Message: "Name is required."}, func(_ context.Context, name string) (bool, error) {
			*stages = append(*stages, "validate")
			return name != "", nil
		})),
	))
	endpoint = foundryhttp.PrepareEndpoint(endpoint, func(_ context.Context, in pipelineInput) (foundryhttp.NoQuery, pipelineForm, error) {
		*stages = append(*stages, "prepare")
		in.Body.Name = strings.TrimSpace(in.Body.Name)
		in.Body.Secret = value.Optional[string]{}
		return in.Query, in.Body, nil
	})
	endpoint = foundryhttp.AuthorizeEndpoint(endpoint, func(_ context.Context, in pipelineInput) error {
		*stages = append(*stages, "authorize")
		if in.Body.Name == "deny" {
			return foundryhttp.Forbidden
		}
		return nil
	})
	return endpoint
}

func TestRequestEndpointKindsPreserveLifecycleAndResponse(t *testing.T) {
	for _, kind := range []string{"ordinary", "authenticated", "bound", "streaming"} {
		t.Run(kind, func(t *testing.T) {
			var stages []string
			access := foundryhttp.Public
			if kind == "authenticated" {
				access = foundryhttp.Guarded
			}
			endpoint := foundryhttp.EndpointHeaders(pipelineEndpoint(foundryhttp.EmptyResponse(204), &stages, access), func(context.Context, foundryhttp.NoContent) ([]foundryhttp.ResponseHeader, error) {
				stages = append(stages, "headers")
				return []foundryhttp.ResponseHeader{{Name: "X-Lifecycle", Value: "ready"}}, nil
			})
			handler := func(_ context.Context, in pipelineInput) (foundryhttp.NoContent, error) {
				stages = append(stages, "handler")
				if in.Body.Name != "Jane" {
					t.Error("handler did not receive prepared input")
				}
				return foundryhttp.NoContent{}, nil
			}
			var registration foundryhttp.RouteRegistration
			if kind != "authenticated" {
				registration = endpoint.Handle(handler)
			}
			wantStatus, wantBody := 204, ""
			success := []string{"decode", "prepare", "authorize"}
			invalid := slices.Clone(success)
			switch kind {
			case "authenticated":
				var loads atomic.Int32
				registry, guard, _ := authSetup(t, "bearer", &loads)
				transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
				if err != nil {
					t.Fatal(err)
				}
				guarded := foundryhttp.AuthorizeActor(foundryhttp.RequireAuthentication(endpoint, transport, guard), func(_ context.Context, actor authAccount, _ pipelineInput) error {
					stages = append(stages, "actor")
					if actor.ID != 1 {
						t.Error("wrong authorized actor")
					}
					return nil
				})
				registration = guarded.Handle(func(ctx context.Context, actor authAccount, in pipelineInput) (foundryhttp.NoContent, error) {
					if actor.ID != 1 {
						t.Error("wrong handler actor")
					}
					return handler(ctx, in)
				})
				success = append(success, "actor")
				invalid = append(invalid, "actor")
			case "bound":
				registration = foundryhttp.HandleBound(endpoint, func(_ context.Context, in pipelineInput) (foundryhttp.Handler[foundryhttp.NoPath, foundryhttp.NoQuery, pipelineForm, foundryhttp.NoContent], error) {
					stages = append(stages, "bind")
					if in.Body.Name != "Jane" && in.Body.Name != "" {
						t.Error("binding did not receive prepared input")
					}
					return handler, nil
				})
				success = append(success, "bind")
				invalid = append(invalid, "bind")
			case "streaming":
				stream := pipelineEndpoint(foundryhttp.StreamResponse("text/plain"), &stages, access)
				registration = stream.Handle(func(_ context.Context, in pipelineInput) (foundryhttp.Stream, error) {
					stages = append(stages, "handler")
					if in.Body.Name != "Jane" {
						t.Error("stream handler did not receive prepared input")
					}
					return foundryhttp.StreamFrom(func(context.Context) (foundryhttp.StreamContent, error) {
						stages = append(stages, "open")
						return foundryhttp.StreamContent{Body: &pipelineReader{Reader: strings.NewReader("Jane"), close: func() { stages = append(stages, "response-close") }}, Name: "result.txt", MediaType: "text/plain", Length: value.Set(int64(4))}, nil
					}), nil
				})
				wantStatus, wantBody = 200, "Jane"
			}
			success = append(success, "validate", "handler")
			if kind == "streaming" {
				success = append(success, "open")
			} else {
				success = append(success, "headers")
			}
			success = append(success, "publish")
			if kind == "streaming" {
				success = append(success, "response-close")
			}
			invalid = append(invalid, "validate", "publish")
			router, err := foundryhttp.NewRouter(registration)
			if err != nil {
				t.Fatal(err)
			}
			metadata := router.Endpoints()
			if len(metadata) != 1 || metadata[0].Status != wantStatus || !metadata[0].Preparation || metadata[0].Validation == nil || metadata[0].Body == nil || metadata[0].Route.ID != "pipeline" {
				t.Fatal("endpoint metadata changed", metadata)
			}
			for _, tc := range []struct {
				body   string
				status int
				order  []string
			}{
				{"name=++Jane++", wantStatus, success},
				{"name=Jane&secret=original", 422, []string{"decode", "publish"}},
				{"name=%zz", 400, []string{"decode", "publish"}},
				{"name=deny", 403, []string{"decode", "prepare", "authorize", "publish"}},
				{"name=++", 422, invalid},
			} {
				stages = nil
				req := httptest.NewRequest("POST", "/pipeline", &pipelineReader{Reader: strings.NewReader(tc.body), first: func() { stages = append(stages, "decode") }})
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Authorization", "Bearer valid")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(pipelineWriter{recorder, &stages}, req)
				if recorder.Code != tc.status || !slices.Equal(stages, tc.order) {
					t.Fatalf("%s: status %d, stages %v, body %s", tc.body, recorder.Code, stages, recorder.Body.String())
				}
				if tc.status == wantStatus {
					if recorder.Body.String() != wantBody {
						t.Fatal("response body changed", recorder.Body.String())
					}
					if kind == "streaming" {
						if recorder.Header().Get("Content-Type") != "text/plain" || recorder.Header().Get("Content-Length") != "4" {
							t.Fatal("stream representation changed", recorder.Header())
						}
					} else if recorder.Header().Get("X-Lifecycle") != "ready" {
						t.Fatal("response headers changed", recorder.Header())
					}
				}
			}
		})
	}
}
