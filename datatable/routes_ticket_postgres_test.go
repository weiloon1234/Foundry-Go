package datatable

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresTokenRoutesKeepScopeThroughDeferredDownload(t *testing.T) {
	fixture := reportPostgres(t)
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, migration := range tokenpg.Migrations() {
			for _, statement := range migration.SQL {
				if _, err := tx.Exec(ctx, statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	backendConfig := tokenpg.DefaultConfig()
	backendConfig.Schema = fixture.Schema
	backend, err := tokenpg.New(fixture.DB, backendConfig)
	if err != nil {
		t.Fatal(err)
	}
	tokenConfig := token.DefaultConfig(keyspace.Namespace{Application: "table-tickets", Environment: "test"})
	store, err := token.NewStore(backend, tokenConfig)
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("table.actors", routeReference(0), func(_ context.Context, id int64) (value.Optional[routeActor], error) {
		return value.Set(routeActor{ID: id, Allowed: id != 8}), nil
	}, func(context.Context, routeActor) (bool, error) { return true, nil })
	scopes, err := auth.NewAccessScopes(auth.DefineAccessScope[routeActor]("tables.export"))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "table.guard", provider, "tables.bearer", scopes)
	if err != nil {
		t.Fatal(err)
	}
	permission := auth.DefinePermission("tables.export", func(_ context.Context, actor routeActor) (bool, error) { return actor.Allowed, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), tokens.Guard().Registration(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential(tokens.TicketSource()))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, tokens.Guard())
	if err != nil {
		t.Fatal(err)
	}
	spec := reportSpec()
	source := spec.Source
	deferred := false
	spec.Source = func(ctx context.Context, authority reportActor) (rows.Query[reportRecord, ReportRow], error) {
		actor, err := tokens.Guard().RequireScopes(ctx, scopes)
		if err != nil {
			return rows.Query[reportRecord, ReportRow]{}, err
		}
		if actor.ID != 7 || authority.Tenant != 8 {
			t.Error("deferred actor/locale lost")
		}
		deferred = true
		return source(ctx, authority)
	}
	manager, table := reportManager(t, fixture, spec, nil)
	config, _ := routeConfig(t, manager)
	config.Guard = binding
	config.Tickets = TokenTickets(tokens)
	config.WithLocale = func(authority reportActor, locale i18n.LocaleID) reportActor {
		if locale == "ms" {
			authority.Tenant = 8
		}
		return authority
	}
	proof, err := auth.NewProof(routeReference(7), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := tokens.Issue(t.Context(), proof, token.IssueOptions[routeActor]{Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	router := routeRouter(t, table, config, ExportAccess(h.Scopes(scopes)))
	response := routeRequest(t, router, "POST", "https://example.test/tables/reports.members/export", `{"format":"csv","request":{}}`, issued.AccessSecret().Reveal(), 200)
	var ticket ExportTicket
	if err := json.Unmarshal(response.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	download := routeRequest(t, router, "GET", "https://example.test"+ticket.URL+"&locale=ms", "", "ignored", 200)
	if !deferred || !strings.Contains(download.Body.String(), "Foreign") || !strings.Contains(download.Body.String(), "ms:") {
		t.Fatal("deferred download/locale failed")
	}
	if download.Result().Header.Get("Cache-Control") != "no-store" || download.Result().Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("download replaced privacy headers")
	}
	routeRequest(t, router, "GET", "https://example.test"+ticket.URL, "", "ignored", 401)
	t.Run("download-export-access-independent-of-table-authorization", func(t *testing.T) {
		allowing := reportSpec()
		tableAuthorizations := 0
		allowing.Authorize = func(context.Context, reportActor, Action) error {
			tableAuthorizations++
			return nil
		}
		manager, table := reportManager(t, fixture, allowing, nil)
		isolated := config
		isolated.Manager = manager
		isolated.Authority = func(context.Context, routeActor) (reportActor, error) {
			return reportActor{Tenant: 7, Allowed: true}, nil
		}
		guarded := routeRouter(t, table, isolated, ExportAccess(h.Scopes(scopes), h.Permissions(permission)))
		tableOnly := routeRouter(t, table, isolated)
		for _, tc := range []struct {
			name   string
			id     int64
			scopes auth.AccessScopes[routeActor]
		}{
			{"scope", 7, auth.AccessScopes[routeActor]{}},
			{"permission", 8, scopes},
		} {
			t.Run(tc.name, func(t *testing.T) {
				proof, err := auth.NewProof(routeReference(tc.id), auth.Authenticated)
				if err != nil {
					t.Fatal(err)
				}
				access, err := tokens.Issue(t.Context(), proof, token.IssueOptions[routeActor]{Scopes: tc.scopes})
				if err != nil {
					t.Fatal(err)
				}
				location := func() string {
					t.Helper()
					credentials, err := auth.NewCredentials(auth.Credential{Name: tokens.TicketSource(), Secret: access.AccessSecret()})
					if err != nil {
						t.Fatal(err)
					}
					scope, err := registry.NewScope(t.Context(), credentials)
					if err != nil {
						t.Fatal(err)
					}
					defer scope.Close()
					ticket, err := tokens.IssueTicket(scope.Context())
					if err != nil {
						t.Fatal(err)
					}
					return "https://example.test/tables/reports.members/download?format=csv&request=%7B%7D&ticket=" + url.QueryEscape(ticket.Secret().Reveal())
				}
				before := tableAuthorizations
				denied := routeRequest(t, guarded, "GET", location(), "", "ignored", 403)
				if tableAuthorizations != before || denied.Result().Header.Get("Cache-Control") != "no-store" || denied.Result().Header.Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("export access did not deny before table authorization")
				}
				// Same actor and token ceiling, without HTTP ExportAccess: the
				// table itself allows the download and actually produces rows.
				allowed := routeRequest(t, tableOnly, "GET", location(), "", "ignored", 200)
				if tableAuthorizations <= before || allowed.Body.Len() == 0 {
					t.Fatal("table authorization control did not allow download")
				}
			})
		}
	})
	disabled := tokenConfig
	disabled.TicketLifetime = 0
	disabled.MaxTicketsPerFamily = 0
	disabledStore, err := token.NewStore(backend, disabled)
	if err != nil {
		t.Fatal(err)
	}
	disabledTokens, err := token.New(disabledStore, "table.guard", provider, "tables.bearer", scopes)
	if err != nil {
		t.Fatal(err)
	}
	config.Tickets = TokenTickets(disabledTokens)
	if registrations, err := Routes(h.Scope{}, table, config); err == nil || registrations != nil {
		t.Fatal("disabled tickets registered")
	}
	other, err := token.New(store, "other", provider, "other.source", scopes)
	if err != nil {
		t.Fatal(err)
	}
	config.Tickets = TokenTickets(other)
	if registrations, err := Routes(h.Scope{}, table, config); err == nil || registrations != nil {
		t.Fatal("wrong-source tickets registered")
	}
	config.Tickets = TokenTickets((*token.Tokens[routeActor, int64])(nil))
	if registrations, err := Routes(h.Scope{}, table, config); err == nil || registrations != nil {
		t.Fatal("nil tickets registered")
	}
}
