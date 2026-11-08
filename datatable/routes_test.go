package datatable

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type routeActor struct {
	ID      int64
	Allowed bool
}

func (a routeActor) FoundryIdentity() (model.Identity, error) { return routeReference(a.ID).Identity() }

func routeReference(id int64) model.Reference[routeActor, int64] {
	return model.NewReference[routeActor]("table_actors", id, codec.Signed[int64]())
}

type routeTicketRedeemer struct {
	redeem func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error)
}

func (routeTicketRedeemer) TicketSource() auth.CredentialName { return "tables.bearer" }
func (r routeTicketRedeemer) RedeemTicket(ctx context.Context, raw secret.String) (value.Optional[auth.BoundCredential], error) {
	return r.redeem(ctx, raw)
}

func routeConfig(t *testing.T, manager *Manager) (RoutesConfig[routeActor, reportActor], []h.GuardOption[routeActor]) {
	t.Helper()
	required, err := auth.NewAccessScopes(auth.DefineAccessScope[routeActor]("tables.read"))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("table.actors", routeReference(0), func(_ context.Context, id int64) (value.Optional[routeActor], error) {
		return value.Set(routeActor{ID: id, Allowed: id != 2}), nil
	}, func(context.Context, routeActor) (bool, error) { return true, nil })
	verify := func(_ context.Context, ticket secret.String) (value.Optional[auth.Proof[routeActor, int64]], error) {
		scopes := required
		id := int64(1)
		if ticket.Reveal() == "scope-denied" {
			scopes = auth.AccessScopes[routeActor]{}
		}
		if ticket.Reveal() == "permission-denied" {
			id = 2
		}
		proof, err := auth.NewScopedProof(routeReference(id), auth.Authenticated, scopes)
		return value.Set(proof), err
	}
	strategy, binder := auth.BindStrategy(auth.DefineStrategy("tables.bearer", verify), verify)
	guard := auth.DefineGuard("table.guard", provider, strategy)
	permission := auth.DefinePermission("table.read", func(_ context.Context, actor routeActor) (bool, error) { return actor.Allowed, nil })
	unavailable := auth.DefinePermission("table.unavailable", func(context.Context, routeActor) (bool, error) { return false, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration(), unavailable.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential("tables.bearer"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	expiry, err := temporal.ParseDateTime("2026-11-01T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return RoutesConfig[routeActor, reportActor]{Manager: manager, Guard: binding, Clock: clock.System{}, Authority: func(_ context.Context, actor routeActor) (reportActor, error) {
		return reportActor{Tenant: 7, Allowed: actor.Allowed}, nil
	}, Tickets: Tickets[routeActor]{issue: func(context.Context) (secret.String, temporal.DateTime, error) {
		return secret.New("one"), expiry, nil
	}, validate: func() error { return nil }, redeemer: routeTicketRedeemer{redeem: func(_ context.Context, raw secret.String) (value.Optional[auth.BoundCredential], error) {
		if raw.Reveal() == "denied" {
			raw = secret.New("permission-denied")
		}
		bound, err := binder.Bind(raw)
		return value.Set(bound), err
	}}}, Quota: h.DefineMiddleware("table.quota", func(next stdhttp.Handler) (stdhttp.Handler, error) { return next, nil })}, []h.GuardOption[routeActor]{h.AnyOf(h.AllOf(h.Scopes(required), h.Permissions(unavailable)), h.AllOf(h.Scopes(required), h.Permissions(permission)))}
}

func routeRouter(t *testing.T, table Table[reportRecord, ReportRow, reportActor], config RoutesConfig[routeActor, reportActor], options ...RouteOption[routeActor]) *h.Router {
	t.Helper()
	registrations, err := Routes(h.DefineScope("/tables", "tables"), table, config, options...)
	if err != nil {
		t.Fatal(err)
	}
	router, err := h.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func routeRequest(t *testing.T, router *h.Router, method, location, body, token string, status int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, location, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, strings.Split(location, "?")[0], response.Code, status, response.Body.String())
	}
	return response
}

func TestRoutesValidateDeclarationsAndSnapshotAccess(t *testing.T) {
	table := Define(reportSpec())
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Dependencies{Database: new(database.DB), Locales: locales, Labels: func(context.Context, i18n.LocaleID, i18n.MessageKey) (string, error) { return "label", nil }}, DefaultConfig(), table.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	config, access := routeConfig(t, manager)
	for _, tc := range []struct {
		name   string
		change func(*RoutesConfig[routeActor, reportActor])
	}{
		{"manager", func(c *RoutesConfig[routeActor, reportActor]) { c.Manager = nil }},
		{"zero-manager", func(c *RoutesConfig[routeActor, reportActor]) { c.Manager = new(Manager) }},
		{"guard", func(c *RoutesConfig[routeActor, reportActor]) { c.Guard = h.GuardBinding[routeActor]{} }},
		{"authority", func(c *RoutesConfig[routeActor, reportActor]) { c.Authority = nil }},
		{"clock", func(c *RoutesConfig[routeActor, reportActor]) { c.Clock = nil }},
		{"issue", func(c *RoutesConfig[routeActor, reportActor]) { c.Tickets.issue = nil }},
		{"redeem", func(c *RoutesConfig[routeActor, reportActor]) { c.Tickets.redeemer = nil }},
		{"quota", func(c *RoutesConfig[routeActor, reportActor]) { c.Quota = h.Middleware{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := config
			tc.change(&bad)
			registrations, err := Routes(h.Scope{}, table, bad)
			if err == nil || registrations != nil {
				t.Fatal("invalid configuration returned routes")
			}
		})
	}
	for _, options := range [][]RouteOption[routeActor]{{ReadAccess[routeActor](), ReadAccess[routeActor]()}, {ExportAccess[routeActor](), ExportAccess[routeActor]()}, {RouteOption[routeActor]{}}, {ReadAccess[routeActor](nil)}, {ExportAccess[routeActor](nil)}} {
		if result, err := Routes(h.Scope{}, table, config, options...); err == nil || result != nil {
			t.Fatal("invalid options returned routes")
		}
	}
	if result, err := Routes(h.DefineScope("/bad/", "bad"), table, config); err == nil || result != nil {
		t.Fatal("invalid scope returned routes")
	}
	if result, err := Routes(h.Scope{}, Define(reportSpec()), config); err == nil || result != nil {
		t.Fatal("different declaration accepted")
	}
	read, export := ReadAccess(access...), ExportAccess(access...)
	for i := range access {
		access[i] = nil
	}
	router := routeRouter(t, table, config, read, export)
	for _, suffix := range []string{"", "/export"} {
		for _, token := range []string{"scope-denied", "permission-denied"} {
			routeRequest(t, router, "POST", "https://example.test/tables/reports.members"+suffix, `{`, token, 403)
		}
	}
	if len(router.Routes()) != 3 {
		t.Fatal("wrong route count")
	}
	for _, route := range router.Routes() {
		if strings.HasSuffix(string(route.ID), ".download") && (route.Access != h.Guarded || route.Timeout != manager.DownloadTimeout()) {
			t.Fatal("download access/deadline", route)
		}
		issuance := strings.HasSuffix(string(route.ID), ".export")
		if slices.Contains(route.Middlewares, h.CredentialRequestsMiddlewareID) != issuance || slices.Contains(route.Middlewares, config.Quota.ID()) != issuance {
			t.Fatal("issuance middleware escaped", route)
		}
	}
}

func TestPostgresRoutesTicketLifecycle(t *testing.T) {
	fixture := reportPostgres(t)
	var localized atomic.Bool
	spec := reportSpec()
	source := spec.Source
	spec.Source = func(ctx context.Context, actor reportActor) (rows.Query[reportRecord, ReportRow], error) {
		if actor.Tenant == 8 {
			localized.Store(true)
		}
		return source(ctx, actor)
	}
	manager, table := reportManager(t, fixture, spec, nil)
	config, access := routeConfig(t, manager)
	var issued, redeemed, quota atomic.Int32
	issue := config.Tickets.issue
	config.Tickets.issue = func(ctx context.Context) (secret.String, temporal.DateTime, error) {
		issued.Add(1)
		return issue(ctx)
	}
	config.Quota = h.DefineMiddleware("table.quota", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { quota.Add(1); next.ServeHTTP(w, r) }), nil
	})
	config.WithLocale = func(actor reportActor, locale i18n.LocaleID) reportActor {
		if locale == "ms" {
			actor.Tenant = 8
		}
		return actor
	}
	baseRedeemer := config.Tickets.redeemer
	config.Tickets.redeemer = routeTicketRedeemer{redeem: func(ctx context.Context, ticket secret.String) (value.Optional[auth.BoundCredential], error) {
		redeemed.Add(1)
		switch ticket.Reveal() {
		case "disabled", "revoked", "expired", "replayed":
			return value.Optional[auth.BoundCredential]{}, auth.Unauthenticated
		}
		return baseRedeemer.RedeemTicket(ctx, ticket)
	}}
	router := routeRouter(t, table, config, ReadAccess(access...), ExportAccess(access...))
	base := "https://example.test/tables/reports.members"
	for _, body := range []string{`{`, `{"format":"pdf","request":{}}`, `{"format":"csv","request":{"sort":[{"column":"absent","direction":"asc"}]}}`, `{"format":"csv","request":{"page":-1}}`} {
		routeRequest(t, router, "POST", base+"/export", body, "valid", 400)
	}
	if issued.Load() != 0 {
		t.Fatal("invalid request issued ticket")
	}
	denied := config
	denied.Authority = func(context.Context, routeActor) (reportActor, error) { return reportActor{}, nil }
	routeRequest(t, routeRouter(t, table, denied), "POST", base+"/export", `{"format":"csv","request":{"page":-1}}`, "valid", 403)
	if issued.Load() != 0 {
		t.Fatal("table authorization did not precede validation and issuance")
	}
	// Percent-encoding expands bounded individual filter values beyond the query limit.
	large, _ := json.Marshal(ExportRequest{Format: CSV, Request: Request{Filters: []Filter{{Column: "name", Op: In, Values: []string{strings.Repeat("?", 3000), strings.Repeat("?", 3000)}}}}})
	routeRequest(t, router, "POST", base+"/export", string(large), "valid", 400)
	if issued.Load() != 0 {
		t.Fatal("oversized URL issued ticket")
	}
	localeContext, err := i18n.WithLocale(t.Context(), manager.dependencies.Locales, "ms")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", base+"/export", strings.NewReader(`{"format":"csv","request":{}}`)).WithContext(localeContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("localized issuance failed", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing credential protection")
	}
	var ticket ExportTicket
	if err := json.Unmarshal(response.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket.ExpiresAt.IsZero() || issued.Load() != 1 {
		t.Fatal("invalid ticket result")
	}
	parsed, err := url.Parse(ticket.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("locale") != "ms" {
		t.Fatal("issuance lost negotiated locale")
	}
	before := quota.Load()
	download := routeRequest(t, router, "GET", "https://example.test"+parsed.String(), "", "valid", 200)
	if !localized.Load() || !strings.Contains(download.Body.String(), "Foreign") || !strings.Contains(download.Body.String(), "ms:") {
		t.Fatal("locale did not reach deferred rows and headings", download.Body.String())
	}
	if redeemed.Load() != 1 || quota.Load() != before {
		t.Fatal("download redemption/quota count")
	}
	if !strings.Contains(download.Header().Get("Content-Disposition"), "reports.members-") {
		t.Fatal("filename convention lost")
	}
	for _, state := range []string{"disabled", "revoked", "expired", "replayed", "denied"} {
		query.Set("ticket", state)
		parsed.RawQuery = query.Encode()
		status := 401
		if state == "denied" {
			status = 403
		}
		routeRequest(t, router, "GET", "https://example.test"+parsed.String(), "", "valid", status)
	}
	// Insecure issuance is refused before quota and callbacks; ordinary queries are not credential submissions.
	before = quota.Load()
	routeRequest(t, router, "POST", "http://example.test/tables/reports.members/export", `{"format":"csv","request":{}}`, "valid", 400)
	if quota.Load() != before {
		t.Fatal("insecure issuance reached quota")
	}
	page := routeRequest(t, router, "POST", "http://example.test/tables/reports.members", `{}`, "valid", 200)
	if quota.Load() != before || page.Header().Get("Referrer-Policy") == "no-referrer" {
		t.Fatal("query acquired issuance policy")
	}
	if !strings.Contains(page.Body.String(), `"total":4`) {
		t.Fatal("wrong page", page.Body.String())
	}
	config.Tickets.issue = func(context.Context) (secret.String, temporal.DateTime, error) {
		issued.Add(1)
		return secret.New(strings.Repeat("x", 20<<10)), temporal.DateTime{}, nil
	}
	router = routeRouter(t, table, config)
	before = issued.Load()
	routeRequest(t, router, "POST", base+"/export", `{"format":"csv","request":{}}`, "valid", 400)
	if issued.Load() != before+1 {
		t.Fatal("post-issuance URL limit not exercised")
	}
	assertExportCapacity(t, manager)
	noReportFiles(t, manager)
}

func TestPostgresRoutesQueryOnlyAndEmptyPage(t *testing.T) {
	fixture := reportPostgres(t)
	spec := reportSpec()
	spec.Exports = false
	manager, table := reportManager(t, fixture, spec, nil)
	config, access := routeConfig(t, manager)
	config.Clock = nil
	config.Quota = h.Middleware{}
	config.Tickets = Tickets[routeActor]{}
	config.Authority = func(context.Context, routeActor) (reportActor, error) {
		return reportActor{Tenant: 999, Allowed: true}, nil
	}
	router := routeRouter(t, table, config, ReadAccess(access...))
	if len(router.Routes()) != 1 {
		t.Fatal("query-only table has export routes")
	}
	response := routeRequest(t, router, "POST", "https://example.test/tables/reports.members", `{}`, "valid", 200)
	if !strings.Contains(response.Body.String(), `"data":[]`) || !strings.Contains(response.Body.String(), `"next":null`) {
		t.Fatal("empty page contract", response.Body.String())
	}
	routeRequest(t, router, "POST", "https://example.test/tables/reports.members/export", `{}`, "valid", 404)
}

func TestPostgresRoutesCallbackFailureAndCancellation(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), func(c *Config) { c.ExportTimeout = 25 * time.Millisecond })
	config, _ := routeConfig(t, manager)
	for _, boundary := range []string{"authority", "issue", "redeem", "locale"} {
		for _, failure := range []string{"panic", "goexit"} {
			t.Run(boundary+"/"+failure, func(t *testing.T) {
				broken := config
				fail := func() {
					if failure == "goexit" {
						runtime.Goexit()
					}
					panic("private-canary")
				}
				method, location, body := "POST", "https://example.test/tables/reports.members/export", `{"format":"csv","request":{}}`
				switch boundary {
				case "authority":
					broken.Authority = func(context.Context, routeActor) (reportActor, error) { fail(); return reportActor{}, nil }
				case "issue":
					broken.Tickets.issue = func(context.Context) (secret.String, temporal.DateTime, error) {
						fail()
						return secret.String{}, temporal.DateTime{}, nil
					}
				case "redeem":
					broken.Tickets.redeemer = routeTicketRedeemer{redeem: func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error) {
						fail()
						return value.Optional[auth.BoundCredential]{}, nil
					}}
				case "locale":
					broken.WithLocale = func(reportActor, i18n.LocaleID) reportActor { fail(); return reportActor{} }
				}
				if boundary == "redeem" || boundary == "locale" {
					method = "GET"
					location = "https://example.test/tables/reports.members/download?ticket=one&format=csv&request=%7B%7D"
					body = ""
				}
				status := 500
				if boundary == "redeem" {
					status = 503
				}
				response := routeRequest(t, routeRouter(t, table, broken), method, location, body, "valid", status)
				if strings.Contains(response.Body.String(), "private-canary") {
					t.Fatal("panic payload disclosed")
				}
			})
		}
	}
	var deadline time.Duration
	config.Tickets.redeemer = routeTicketRedeemer{redeem: func(ctx context.Context, _ secret.String) (value.Optional[auth.BoundCredential], error) {
		end, ok := ctx.Deadline()
		if !ok {
			return value.Optional[auth.BoundCredential]{}, errors.New("missing deadline")
		}
		deadline = time.Until(end)
		<-ctx.Done()
		return value.Optional[auth.BoundCredential]{}, ctx.Err()
	}}
	routeRequest(t, routeRouter(t, table, config), "GET", "https://example.test/tables/reports.members/download?ticket=one&format=csv&request=%7B%7D", "", "valid", 503)
	if deadline <= 0 || deadline > manager.DownloadTimeout() {
		t.Fatal("wrong download deadline", deadline)
	}
	config.Authority = func(ctx context.Context, _ routeActor) (reportActor, error) { return reportActor{}, ctx.Err() }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := tableDownload(table, config)(ctx, routeActor{}, h.Input[h.NoPath, DownloadQuery, h.NoBody]{Query: DownloadQuery{Format: CSV, Request: `{}`}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("client cancellation lost", err)
	}
	assertExportCapacity(t, manager)
	noReportFiles(t, manager)
}

func TestPostgresRoutesCancellationRetainsCallbackOwnership(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	config, _ := routeConfig(t, manager)
	entered, canceled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	config.Tickets.redeemer = routeTicketRedeemer{redeem: func(ctx context.Context, _ secret.String) (value.Optional[auth.BoundCredential], error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return value.Optional[auth.BoundCredential]{}, ctx.Err()
	}}
	router := routeRouter(t, table, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest("GET", "https://example.test/tables/reports.members/download?ticket=one&format=csv&request=%7B%7D", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	go func() { defer close(returned); router.ServeHTTP(response, request) }()
	<-entered
	cancel()
	<-canceled
	select {
	case <-returned:
		t.Error("HTTP abandoned a running redeemer")
	default:
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("redeemer did not drain")
	}
	if response.Code != 503 {
		t.Fatal("cancellation status", response.Code)
	}
	assertExportCapacity(t, manager)
	noReportFiles(t, manager)
}
