package authenticating_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerSignedOrderAndNativeRoutesKeepConcreteModels(t *testing.T) {
	userID, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := model.NewID[models.Order]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: userID, Status: models.StatusActive}
	order := models.Order{ID: orderID, BuyerID: userID}
	var loads, resources atomic.Int32
	provider := auth.DefineProvider("users", user.FoundryReference(), func(_ context.Context, id model.ID[models.User]) (value.Optional[models.User], error) {
		loads.Add(1)
		return value.Set(user), nil
	}, func(_ context.Context, subject models.User) (bool, error) {
		return subject.Status == models.StatusActive, nil
	})
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		if credential.Reveal() != "fixture-token" {
			return value.Optional[auth.Proof[models.User, model.ID[models.User]]]{}, nil
		}
		return value.Set(proof), nil
	})
	registry, guard, _, err := authenticating.AccountGuards(provider, strategy, strategy)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New(strings.Repeat("x", 32))})
	if err != nil {
		t.Fatal(err)
	}
	now := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	signer, err := foundryhttp.NewURLSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	signed := authenticating.SignedOrders(authenticating.Orders(transport, guard), signer)
	resolver := modelbinding.Define(func(_ context.Context, p authenticating.OrderPath) (value.Optional[models.Order], error) {
		resources.Add(1)
		if p.Order != orderID {
			return value.Optional[models.Order]{}, nil
		}
		return value.Set(order), nil
	})
	calls := 0
	bound := authenticating.BoundOrder(signed, resolver, guard, func(ctx context.Context, subject models.User, resource models.Order) error {
		calls++
		identity, present := attribution.FromContext(ctx).Model()
		expected, _ := subject.FoundryIdentity()
		if !present || identity != expected || subject.ID != userID || resource.ID != orderID {
			t.Error("consumer lost typed models or provenance")
		}
		return nil
	})
	native := authenticating.NativeProfileHandler(authenticating.NativeProfile(transport, guard), func(w http.ResponseWriter, r *http.Request, subject models.User, _ foundryhttp.NoPath) {
		calls++
		if subject.ID != userID {
			t.Error("native handler lost model")
		}
		if _, ok := w.(http.Flusher); !ok {
			t.Error("native writer capability lost")
		}
		w.WriteHeader(204)
	})
	router, err := foundryhttp.NewRouter(bound, native)
	if err != nil {
		t.Fatal(err)
	}
	const origin foundryhttp.Origin = "https://consumer.test"
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{origin}}))
	if err != nil {
		t.Fatal(err)
	}
	location, err := signed.URL(t.Context(), origin, authenticating.OrderPath{Order: orderID}, foundryhttp.NoQuery{}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	send := func(url, credential string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", url, nil)
		if credential != "" {
			request.Header.Set("Authorization", "Bearer "+credential)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, url := range []string{location, string(origin) + "/profile/native"} {
		if response := send(url, "fixture-token"); response.Code != 204 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if calls != 2 || loads.Load() != 2 || resources.Load() != 1 {
		t.Fatal("consumer rehydrated subject or resource")
	}
	before := resources.Load()
	if response := send(location, ""); response.Code != 401 || resources.Load() != before {
		t.Fatal("signed URL replaced credentials")
	}
	if response := send(location+"&extra=1", "fixture-token"); response.Code != 403 || resources.Load() != before {
		t.Fatal("invalid signature reached model query")
	}
	other, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	order.BuyerID = other
	if response := send(location, "fixture-token"); response.Code != 403 || calls != 2 {
		t.Fatal("signed route bypassed ownership policy")
	}
	if len(router.Endpoints()) != 1 || router.Endpoints()[0].Route.SignedURL == nil {
		t.Fatal("raw route became DTO or signature metadata lost")
	}
	for _, route := range router.Routes() {
		if route.Authentication == nil || len(route.Authentication.RequiredPermissions) != 1 {
			t.Fatal("consumer route omitted permission metadata")
		}
	}
}
