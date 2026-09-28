package authenticating_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestTokenConsumerLoginRefreshReplayAndCurrentModel(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range tokenpg.Migrations() {
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
	var enabled atomic.Bool
	enabled.Store(true)
	provider := auth.DefineProvider("users", (models.User{}).FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		loads.Add(1)
		if key != id {
			return value.Optional[models.User]{}, nil
		}
		return value.Set(user), nil
	}, func(context.Context, models.User) (bool, error) { return enabled.Load(), nil })
	now := testkit.NewClock(time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC))
	persistence := tokenpg.DefaultConfig()
	persistence.Schema = schema
	persistence.Clock = now
	tokens, err := authenticating.APITokens(db, provider, persistence, token.DefaultConfig(keyspace.Namespace{Application: "token-consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	router, err := authenticating.TokenRoutes(tokens, now, func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error) { return proof, nil })
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://consumer.test"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	type pair struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	decode := func(w *httptest.ResponseRecorder) pair {
		t.Helper()
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("token delivery", w.Code)
		}
		var wire struct {
			Tokens      pair `json:"tokens"`
			MFARequired bool `json:"mfa_required"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		if wire.MFARequired || len(wire.Tokens.Access) != 43 || len(wire.Tokens.Refresh) != 43 {
			t.Fatal("invalid credential pair")
		}
		return wire.Tokens
	}
	refreshBody := func(raw string) string {
		b, _ := json.Marshal(map[string]string{"refresh_token": raw})
		return string(b)
	}
	first := decode(call("POST", "/login", "", ""))
	if w := call("GET", "/profile", "", first.Access); w.Code != 204 {
		t.Fatal("concrete token model", w.Code)
	}
	if loads.Load() != 1 {
		t.Fatal("scope check and handler loaded model more than once")
	}
	listed, err := authenticating.ListUserTokens(t.Context(), tokens, user)
	if err != nil || len(listed) != 1 || authenticating.UserTokenSubject(listed[0]).Key() != id {
		t.Fatal("typed metadata", err)
	}
	originalID := listed[0].ID()
	second := decode(call("POST", "/refresh", refreshBody(first.Refresh), ""))
	if first.Access == second.Access || first.Refresh == second.Refresh {
		t.Fatal("refresh reused secrets")
	}
	if w := call("GET", "/profile", "", first.Access); w.Code != 401 {
		t.Fatal("old access accepted", w.Code)
	}
	if w := call("GET", "/profile", "", second.Access); w.Code != 204 {
		t.Fatal("new access rejected", w.Code)
	}
	listed, err = authenticating.ListUserTokens(t.Context(), tokens, user)
	if err != nil || len(listed) != 1 || listed[0].ID() != originalID || listed[0].Generation() != 1 {
		t.Fatal("refresh changed family identity", err)
	}
	enabled.Store(false)
	if w := call("GET", "/profile", "", second.Access); w.Code != 401 {
		t.Fatal("disabled model accepted", w.Code)
	}
	enabled.Store(true)
	if w := call("POST", "/refresh", refreshBody(first.Refresh), ""); w.Code != 401 {
		t.Fatal("refresh replay accepted", w.Code)
	}
	if w := call("GET", "/profile", "", second.Access); w.Code != 401 {
		t.Fatal("replay revocation did not commit", w.Code)
	}
	if w := call("POST", "/refresh", refreshBody(second.Refresh), ""); w.Code != 401 {
		t.Fatal("revoked refresh accepted", w.Code)
	}
	third := decode(call("POST", "/login", "", ""))
	listed, err = authenticating.ListUserTokens(t.Context(), tokens, user)
	if err != nil || len(listed) != 1 {
		t.Fatal("reissue", err)
	}
	if ok, err := tokens.RevokeID(t.Context(), user.FoundryReference(), listed[0].ID()); err != nil || !ok {
		t.Fatal("typed revocation", err)
	}
	if w := call("GET", "/profile", "", third.Access); w.Code != 401 {
		t.Fatal("revoked credential accepted", w.Code)
	}
}
