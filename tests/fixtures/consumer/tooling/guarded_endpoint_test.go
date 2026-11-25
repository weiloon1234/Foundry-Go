package tooling_test

import (
	"context"
	"testing"
	"time"

	"foundry.test/consumer/models"
	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin guarded-test-backend

// memoryTokens is a token.Backend that keeps issued tokens in memory so a test
// needs no database. Embedding the interface leaves the unused methods unset.
type memoryTokens struct {
	token.Backend
	records []token.Record
}

func (m *memoryTokens) Create(_ context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	record, err := creation.At(address, time.Now())
	if err != nil {
		return token.Record{}, err
	}
	m.records = append(m.records, record)
	return record, nil
}

func (m *memoryTokens) Lookup(_ context.Context, _ token.Address, digest token.Digest, _ bool) (value.Optional[token.Record], error) {
	for _, record := range m.records {
		if record.AccessHash.Equal(digest) {
			return value.Set(record), nil
		}
	}
	return value.Optional[token.Record]{}, nil
}

// doc-example:end guarded-test-backend

// doc-example:begin guarded-test-handler

var publishScope = auth.DefineAccessScope[models.User]("accounts.create")

// guardedAccounts serves POST /accounts for a user holding the accounts.create scope.
func guardedAccounts(t *testing.T) (*httptest.Client, *token.Tokens[models.User, model.ID[models.User]]) {
	t.Helper()
	provider := auth.DefineProvider("users", orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(models.User{}), func(_ context.Context, id model.ID[models.User]) (value.Optional[models.User], error) {
		return value.Set(models.User{ID: id, Status: models.StatusActive}), nil
	}, func(_ context.Context, user models.User) (bool, error) {
		return user.Status == models.StatusActive, nil
	})
	store, err := token.NewStore(&memoryTokens{}, token.DefaultConfig(keyspace.Namespace{Application: "tooling", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := auth.NewAccessScopes(publishScope)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "users.api", provider, "users.bearer", scopes)
	if err != nil {
		t.Fatal(err)
	}
	guard := tokens.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := foundryhttp.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "accounts.create", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/accounts"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), outgoing.CreateAccountBody(), outgoing.AccountReceiptResponse(201))
	endpoint = foundryhttp.ValidateBody(endpoint, validation.Rules(outgoing.CreateAccountValidationFields().Name, validation.NonBlank[string]()))
	guarded := foundryhttp.Authenticated(endpoint, binding, foundryhttp.AllOf(foundryhttp.Scopes(scopes)))
	router, err := foundryhttp.NewRouter(guarded.Handle(func(_ context.Context, user models.User, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, outgoing.CreateAccount]) (outgoing.AccountReceipt, error) {
		return outgoing.AccountReceipt{ID: 7, Name: input.Body.Name}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return httptest.New(t, router), tokens
}

// doc-example:end guarded-test-handler

func TestGuardedEndpointStatuses(t *testing.T) {
	client, tokens := guardedAccounts(t)
	var err error
	user := models.User{Status: models.StatusActive}
	user.ID, err = model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := auth.NewAccessScopes(publishScope)
	if err != nil {
		t.Fatal(err)
	}
	// doc-example:begin guarded-test-calls
	post := func(client *httptest.Client, name string) httpclient.Response {
		request, err := httptest.JSON(t.Context(), client.Post("/accounts"), outgoing.CreateAccountJSON(), outgoing.CreateAccount{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	httptest.AssertStatus(t, post(client, "Ada"), 401)

	scoped := httptest.ActingAsToken(t, client, tokens, proof, token.IssueOptions[models.User]{Name: "test", Scopes: scopes})
	created := post(scoped, "Ada")
	httptest.AssertStatus(t, created, 201)
	receipt, err := httptest.DecodeJSON(t.Context(), created, outgoing.AccountReceiptJSON())
	if err != nil || receipt.ID != 7 || receipt.Name != "Ada" {
		t.Fatal("unexpected receipt", receipt, err)
	}
	httptest.AssertStatus(t, post(scoped, " "), 422)

	unscoped := httptest.ActingAsToken(t, client, tokens, proof, token.IssueOptions[models.User]{Name: "no scope"})
	httptest.AssertStatus(t, post(unscoped, "Ada"), 403)
	// doc-example:end guarded-test-calls
}
