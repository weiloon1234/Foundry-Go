package http

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

type formOwnedCodec struct{ parse func(string) (string, error) }

func (c formOwnedCodec) Parse(text string) (string, error) { return c.parse(text) }
func (formOwnedCodec) Format(text string) (string, error)  { return text, nil }
func TestFormScalarCallbacksStayOwned(t *testing.T) {
	for _, mode := range []string{"panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			codec := formOwnedCodec{func(string) (string, error) {
				switch mode {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					close(entered)
					<-release
				}
				return "ok", nil
			}}
			endpoint := formEndpoint()
			endpoint.body = FormBody(DefineQuery(QueryParam("name", codec, func(b *formInput) *string { return &b.Name })))
			router, err := NewRouter(endpoint.Handle(func(context.Context, formRequest) (NoContent, error) {
				t.Error("failed scalar reached handler")
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req := httptest.NewRequest("POST", "/form", strings.NewReader("name=ok")).WithContext(ctx)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res := httptest.NewRecorder()
			returned := false
			go func() { defer close(done); router.ServeHTTP(res, req); returned = true }()
			if mode == "cancel" {
				<-entered
				cancel()
				select {
				case <-done:
					t.Fatal("scalar callback abandoned")
				case <-time.After(20 * time.Millisecond):
				}
				close(release)
			}
			<-done
			if mode == "goexit" {
				// Scalar codecs run on the request goroutine (callback.Invoke).
				if returned || res.Body.Len() != 0 {
					t.Fatal("codec Goexit was converted into a response")
				}
				return
			}
			want := 500
			if mode == "cancel" {
				want = 408
			}
			if res.Code != want {
				t.Fatalf("%d %s", res.Code, res.Body.String())
			}
		})
	}
}
