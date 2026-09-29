package http

import (
	"bytes"
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRequestLifecycleOrderAndOriginalProhibitions(t *testing.T) {
	name := validation.DefineField("name", func(b EndpointPatch) string { return b.Name })
	note := validation.DefineField("note", func(b EndpointPatch) value.Optional[value.Nullable[string]] { return b.Note })
	for _, tc := range []struct {
		name, body string
		prohibited bool
		status     int
		sequence   string
	}{
		{"normalize", `{"name":"  Jane  ","note":null}`, false, 201, "prepare,authorize,handler"},
		{"omitted", `{"name":"  Jane  "}`, false, 201, "prepare,authorize,handler"},
		{"invalid-normalized", `{"name":"    "}`, false, 422, "prepare,authorize"},
		{"structural", `{"name":123}`, false, 400, ""},
		{"forbidden-original", `{"name":"Jane","note":"cannot erase"}`, true, 422, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stages []string
			endpoint := patchEndpoint().WithBodyValidation(name.Rules(validation.MinLength[string](2)))
			if tc.prohibited {
				endpoint = endpoint.WithBodyValidation(note.Rules(validation.Absent[value.Nullable[string]]()))
			}
			endpoint = endpoint.WithPreparation(func(_ context.Context, in endpointRequest) (endpointParameters, EndpointPatch, error) {
				stages = append(stages, "prepare")
				in.Body.Name = strings.TrimSpace(in.Body.Name)
				if tc.prohibited {
					in.Body.Note = value.Optional[value.Nullable[string]]{}
				}
				if !in.Query.Term.IsSet() {
					in.Query.Term = value.Set("default")
				}
				return in.Query, in.Body, nil
			}).WithAuthorization(func(_ context.Context, in endpointRequest) error {
				stages = append(stages, "authorize")
				q, _ := in.Query.Term.Get()
				// Authorization precedes validation: it sees prepared input
				// that validation has not checked yet.
				wantName := "Jane"
				if tc.name == "invalid-normalized" {
					wantName = ""
				}
				if in.Path.User.String() != endpointUserID || in.Body.Name != wantName || q != "default" {
					t.Error("authorization did not receive prepared input")
				}
				return nil
			})
			router, err := NewRouter(endpoint.Handle(func(_ context.Context, in endpointRequest) (EndpointReply, error) {
				stages = append(stages, "handler")
				note, present := in.Body.Note.Get()
				if tc.name == "normalize" && (!present || !note.IsNull()) || tc.name == "omitted" && present {
					t.Error("preparation lost nullable presence")
				}
				return EndpointReply{Name: in.Body.Name}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status || strings.Join(stages, ",") != tc.sequence {
				t.Fatalf("%d %v: %s", res.Code, stages, res.Body.String())
			}
		})
	}
}

type lifecycleUnsafeError struct{}

func (lifecycleUnsafeError) Error() string { panic("error formatter must not run") }
func TestRequestLifecycleContainmentAndDeclarations(t *testing.T) {
	for _, stage := range []string{"prepare", "authorize"} {
		for _, failure := range []string{"deny", "error", "panic", "goexit"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				fail := func() error {
					switch failure {
					case "deny":
						return Forbidden
					case "error":
						return lifecycleUnsafeError{}
					case "panic":
						panic("private callback payload")
					case "goexit":
						runtime.Goexit()
					}
					return nil
				}
				endpoint := patchEndpoint()
				if stage == "prepare" {
					endpoint = endpoint.WithPreparation(func(_ context.Context, in endpointRequest) (endpointParameters, EndpointPatch, error) {
						return in.Query, in.Body, fail()
					})
				} else {
					endpoint = endpoint.WithAuthorization(func(context.Context, endpointRequest) error { return fail() })
				}
				router, err := NewRouter(endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
					t.Error("failed hook reached handler")
					return EndpointReply{}, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				want := 500
				if failure == "deny" {
					want = 403
				}
				if res.Code != want || strings.Contains(res.Body.String(), "private") {
					t.Fatalf("%d %s", res.Code, res.Body.String())
				}
			})
		}
	}
	if patchEndpoint().WithPreparation(nil).Validate() == nil || patchEndpoint().WithAuthorization(nil).Validate() == nil {
		t.Fatal("nil callback accepted")
	}
	if err := patchEndpoint().Validate(); err != nil {
		t.Fatal("fluent copy changed original", err)
	}
}

func TestRequestLifecycleMultipartCleanupAndCancellation(t *testing.T) {
	for _, stage := range []string{"prepare", "authorize"} {
		for _, cancelRequest := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "-deny", true: "-cancel"}[cancelRequest], func(t *testing.T) {
				directory := t.TempDir()
				entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var captured UploadedFile
				hook := func(in multipartRequestInput) error {
					captured = in.Body.Primary
					if cancelRequest {
						close(entered)
						<-release
					}
					return Forbidden
				}
				endpoint := multipartEndpoint(directory)
				if stage == "prepare" {
					endpoint = endpoint.WithPreparation(func(_ context.Context, in multipartRequestInput) (NoQuery, multipartRequest, error) {
						return in.Query, in.Body, hook(in)
					})
				} else {
					endpoint = endpoint.WithAuthorization(func(_ context.Context, in multipartRequestInput) error { return hook(in) })
				}
				router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
					t.Error("hook failure reached handler")
					return NoContent{}, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				wire, media := multipartWire(t, primaryUpload("owned resource"))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire)).WithContext(ctx)
				req.Header.Set("Content-Type", media)
				res := httptest.NewRecorder()
				go func() { defer close(finished); router.ServeHTTP(res, req) }()
				if cancelRequest {
					<-entered
					cancel()
					select {
					case <-finished:
						t.Fatal("cancellation abandoned callback")
					case <-time.After(20 * time.Millisecond):
					}
					close(release)
				}
				<-finished
				// The hook's returned denial is authoritative even when the
				// request was canceled while it ran.
				want := 403
				if res.Code != want {
					t.Fatalf("%d %s", res.Code, res.Body.String())
				}
				assertMultipartCleanup(t, directory)
				if reader, err := captured.Open(t.Context()); err == nil {
					reader.Close()
					t.Fatal("upload remained usable after request")
				}
			})
		}
	}
}

func BenchmarkRequestLifecycle(b *testing.B) {
	for _, hooks := range []bool{false, true} {
		b.Run(map[bool]string{false: "NoHooks", true: "PreparationAndAuthorization"}[hooks], func(b *testing.B) {
			endpoint := formEndpoint()
			if hooks {
				endpoint = endpoint.WithPreparation(func(_ context.Context, in formRequest) (endpointParameters, formInput, error) {
					return in.Query, in.Body, nil
				}).WithAuthorization(func(context.Context, formRequest) error { return nil })
			}
			router, err := NewRouter(endpoint.Handle(func(context.Context, formRequest) (NoContent, error) { return NoContent{}, nil }))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				req := httptest.NewRequest("POST", "/form", strings.NewReader("name=sample"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != 204 {
					b.Fatal(res.Code)
				}
			}
		})
	}
}
