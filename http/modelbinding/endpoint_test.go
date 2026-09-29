package modelbinding_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type memberKey int64
type member struct {
	ID          memberKey
	StoredEmail string
}
type path struct{ Member memberKey }
type input = modelbinding.Input[path, foundryhttp.NoQuery, foundryhttp.NoBody, member]

func endpoint() foundryhttp.Endpoint[path, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent] {
	return foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
			foundryhttp.DefinePath("/members/{member}", foundryhttp.Param("member", foundryhttp.IntegerPath[memberKey](), func(p *path) *memberKey { return &p.Member }))),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204),
	).WithPathValidation(validation.DefineField("member", func(p path) memberKey { return p.Member }).Rules(validation.Min(memberKey(1))))
}
func router(t *testing.T, registration foundryhttp.RouteRegistration) *foundryhttp.Router {
	t.Helper()
	r, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func serve(handler http.Handler, url string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	return w
}

func TestBoundEndpointResolvesOncePerAcceptedPath(t *testing.T) {
	var resolved, handled int
	resolver := modelbinding.Define(func(ctx context.Context, p path) (value.Optional[member], error) {
		resolved++
		if ctx == nil {
			t.Fatal("missing request context")
		}
		return value.Set(member{ID: p.Member, StoredEmail: "private"}), nil
	})
	bound := modelbinding.Bind(endpoint(), resolver)
	before, err := endpoint().Description()
	if err != nil {
		t.Fatal(err)
	}
	after, err := bound.Description()
	if err != nil {
		t.Fatal(err)
	}
	if before.Route.ID != after.Route.ID || len(before.Path) != len(after.Path) || after.Response != nil || after.Body != nil {
		t.Fatal("model leaked into wire metadata")
	}
	r := router(t, bound.Handle(func(ctx context.Context, in input) (foundryhttp.NoContent, error) {
		handled++
		if in.Request.Path.Member != 42 || in.Model.ID != 42 || in.Model.StoredEmail != "private" {
			t.Fatal("lost typed model/input")
		}
		return foundryhttp.NoContent{}, nil
	}))
	for _, test := range []struct {
		url    string
		status int
	}{{"/members/invalid", 400}, {"/members/0", 422}, {"/members/42?undeclared=true", 400}, {"/members/42", 204}} {
		w := serve(r, test.url)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.url, w.Code, w.Body.String())
		}
	}
	// Binding precedes validation: the decoded /members/0 is looked up before
	// its rule reports 422; the accepted request is looked up exactly once.
	if resolved != 2 || handled != 1 {
		t.Fatalf("lookup/handler counts %d/%d", resolved, handled)
	}
	if len(r.Endpoints()) != 1 || r.Endpoints()[0].Route.ID != after.Route.ID {
		t.Fatal("bound endpoint lost inspection")
	}
}

func TestModelLookupFailureNeverPublishesPartialModels(t *testing.T) {
	cause := errors.New("private lookup failure")
	for _, mode := range []string{"missing", "error", "partial", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			resolver := modelbinding.Define(func(context.Context, path) (value.Optional[member], error) {
				switch mode {
				case "missing":
					return value.Optional[member]{}, nil
				case "partial":
					return value.Set(member{ID: 99}), cause
				case "panic":
					panic("private panic")
				case "goexit":
					runtime.Goexit()
				}
				return value.Optional[member]{}, cause
			})
			r := router(t, modelbinding.Bind(endpoint(), resolver).Handle(func(context.Context, input) (foundryhttp.NoContent, error) {
				t.Error("handler ran after failed lookup")
				return foundryhttp.NoContent{}, nil
			}))
			w := serve(r, "/members/42")
			want := 500
			if mode == "missing" {
				want = 404
			}
			if w.Code != want || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("unsafe failure %d %s", w.Code, w.Body.String())
			}
			result, err := resolver.Resolve(t.Context(), path{42})
			if result.ID != 0 || err == nil {
				t.Fatal("failed resolver exposed model")
			}
			if (mode == "error" || mode == "partial") && !errors.Is(err, cause) {
				t.Fatal("private cause lost")
			}
		})
	}
}

func TestModelResolverOwnsCanceledLookupUntilReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	resolver := modelbinding.Define(func(context.Context, path) (value.Optional[member], error) {
		close(started)
		<-release
		return value.Set(member{ID: 42}), nil
	})
	go func() {
		m, err := resolver.Resolve(ctx, path{42})
		if m.ID != 0 {
			err = errors.New("published canceled model")
		}
		finished <- err
	}()
	<-started
	cancel()
	select {
	case <-finished:
		t.Fatal("abandoned active lookup")
	default:
	}
	close(release)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lookup did not finish")
	}
	if _, err := resolver.Resolve(nil, path{42}); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted")
	}
}

type neverExecutor struct{}

func (*neverExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	panic("query fake unexpectedly executed SQL")
}
func (*neverExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	panic("query fake unexpectedly executed SQL")
}

type keyedQuery struct {
	find    func(context.Context, database.Executor, memberKey) (value.Optional[member], error)
	invalid bool
}

func (q keyedQuery) Validate() error {
	if q.invalid {
		return errors.New("invalid query")
	}
	return nil
}
func (q keyedQuery) Find(ctx context.Context, db database.Executor, k memberKey) (value.Optional[member], error) {
	return q.find(ctx, db, k)
}

func TestKeyBindingRetainsQueryAndKeyOwnership(t *testing.T) {
	executor := &neverExecutor{}
	calls := 0
	query := keyedQuery{find: func(ctx context.Context, db database.Executor, k memberKey) (value.Optional[member], error) {
		calls++
		if ctx != t.Context() || db != executor || k != 42 {
			t.Fatal("key/context/executor changed")
		}
		return value.Set(member{ID: k}), nil
	}}
	key := func(p path) memberKey { return p.Member }
	resolver := modelbinding.ByKey(executor, query, key)
	if err := resolver.Validate(); err != nil || calls != 0 {
		t.Fatal("assembly resolved a model", err)
	}
	got, err := resolver.Resolve(t.Context(), path{42})
	if err != nil || got.ID != 42 || calls != 1 {
		t.Fatal(got, err, calls)
	}
	var nilExecutor *neverExecutor
	var nilQuery *keyedQuery
	for _, invalid := range []modelbinding.Resolver[path, member]{
		{}, modelbinding.Define[path, member](nil), modelbinding.ByKey(nilExecutor, query, key), modelbinding.ByKey(executor, nilQuery, key),
		modelbinding.ByKey[path](executor, query, nil), modelbinding.ByKey(executor, keyedQuery{invalid: true}, key),
	} {
		if invalid.Validate() == nil {
			t.Fatal("invalid binding accepted")
		}
		if _, err := foundryhttp.NewRouter(modelbinding.Bind(endpoint(), invalid).Handle(func(context.Context, input) (foundryhttp.NoContent, error) { return foundryhttp.NoContent{}, nil })); err == nil {
			t.Fatal("invalid binding registered")
		}
	}
	if _, err := foundryhttp.NewRouter(modelbinding.Bind(endpoint(), resolver).Handle(nil)); err == nil {
		t.Fatal("nil handler registered")
	}
	badSelector := modelbinding.ByKey(executor, query, func(path) memberKey { panic("private selector") })
	if _, err := badSelector.Resolve(t.Context(), path{42}); !errors.Is(err, foundryhttp.InternalError) {
		t.Fatal("selector panic escaped", err)
	}
}

func TestBoundModelResultsAreRequestLocal(t *testing.T) {
	var resolved atomic.Int32
	resolver := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		resolved.Add(1)
		return value.Set(member{ID: p.Member}), nil
	})
	r := router(t, modelbinding.Bind(endpoint(), resolver).Handle(func(_ context.Context, in input) (foundryhttp.NoContent, error) {
		if in.Model.ID != in.Request.Path.Member {
			t.Error("another request's model")
		}
		return foundryhttp.NoContent{}, nil
	}))
	var group sync.WaitGroup
	for i := 1; i <= 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			w := serve(r, "/members/"+strconv.Itoa(i))
			if w.Code != 204 {
				t.Error(w.Code)
			}
		}()
	}
	group.Wait()
	if resolved.Load() != 12 {
		t.Fatal("lookup cached across requests", resolved.Load())
	}
}

func TestSignedModelEndpointVerifiesBeforeLookup(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := foundryhttp.NewURLSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	const origin foundryhttp.Origin = "https://example.test"
	signed := endpoint().Signed(signer)
	location, err := signed.URL(t.Context(), origin, path{42}, foundryhttp.NoQuery{}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	resolver := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		calls++
		return value.Set(member{ID: p.Member}), nil
	})
	bound := modelbinding.Bind(signed, resolver)
	info, err := bound.Description()
	if err != nil || info.Route.SignedURL == nil {
		t.Fatal("signed metadata lost", err)
	}
	r := router(t, bound.Handle(func(context.Context, input) (foundryhttp.NoContent, error) { return foundryhttp.NoContent{}, nil }))
	handler, err := foundryhttp.ApplyMiddleware(r, foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{origin}, Canonical: value.Set(origin)}))
	if err != nil {
		t.Fatal(err)
	}
	if w := serve(handler, strings.Replace(location, "/42?", "/43?", 1)); w.Code != 403 || calls != 0 {
		t.Fatal("unverified URL resolved model", w.Code, calls)
	}
	if w := serve(handler, location); w.Code != 204 || calls != 1 {
		t.Fatal("signed model binding failed", w.Code, w.Body.String(), calls)
	}
}
