package authenticating_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPersistentSessionSuppliesConcreteModel(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range sessionpg.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	var loads atomic.Int32
	provider := auth.DefineProvider("users", (models.User{}).FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		loads.Add(1)
		if key != id {
			return value.Optional[models.User]{}, nil
		}
		return value.Set(user), nil
	}, func(_ context.Context, user models.User) (bool, error) {
		return user.Status == models.StatusActive, nil
	})
	persistence := sessionpg.DefaultConfig()
	persistence.Schema = schema
	sessions, err := authenticating.WebSessions(db, provider, persistence, session.DefaultConfig(keyspace.Namespace{Application: "session-consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := authenticating.StartVerifiedSession(t.Context(), sessions, proof)
	if err != nil {
		t.Fatal(err)
	}
	guard := sessions.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: guard.Source(), Secret: issued.Secret()})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	for range 2 {
		got, err := guard.Require(scope.Context())
		if err != nil || got.ID != user.ID {
			t.Fatal("concrete session user", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatal("provider resolved more than once")
	}
	listed, err := authenticating.ListedSessions(t.Context(), sessions, user)
	if err != nil || len(listed) != 1 {
		t.Fatal("list", err)
	}
	if authenticating.SessionSubject(listed[0]).Key() != id {
		t.Fatal("session reference lost model key")
	}
	if removed, err := authenticating.RevokeListedSession(t.Context(), sessions, user, listed[0].ID()); err != nil || !removed {
		t.Fatal("revoke by typed ID", err)
	}
	next, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := guard.Require(next.Context()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("new scope accepted revoked session", err)
	}
	t.Run("browser", func(t *testing.T) {
		web, err := foundryhttp.NewBrowserSessions(registry, sessions, foundryhttp.DefaultBrowserSessionConfig())
		if err != nil {
			t.Fatal(err)
		}
		router, err := authenticating.BrowserRoutes(web, func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error) { return proof, nil })
		if err != nil {
			t.Fatal(err)
		}
		call := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, "https://consumer.test"+path, nil)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			return w
		}
		first := call("POST", "/login", nil)
		if first.Code != 204 || len(first.Result().Cookies()) != 1 {
			t.Fatal("browser login", first.Code, first.Body.String())
		}
		cookie := first.Result().Cookies()[0]
		if got := call("GET", "/profile", cookie); got.Code != 204 {
			t.Fatal("browser model", got.Code)
		}
		rotated := call("POST", "/rotate", cookie)
		if rotated.Code != 204 || len(rotated.Result().Cookies()) != 1 {
			t.Fatal("browser rotation", rotated.Code)
		}
		next := rotated.Result().Cookies()[0]
		if next.Value == cookie.Value {
			t.Fatal("rotation did not replace cookie")
		}
		if got := call("POST", "/logout", next); got.Code != 204 || len(got.Result().Cookies()) != 1 || got.Result().Cookies()[0].MaxAge != -1 {
			t.Fatal("browser logout", got.Code)
		}
		if got := call("GET", "/profile", next); got.Code != 401 {
			t.Fatal("revoked browser accepted", got.Code)
		}
	})

}
