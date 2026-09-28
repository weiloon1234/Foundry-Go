package http_test

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestTypedHTTPEndpointsAutomaticallyAttributeOnlyVerifiedModels(t *testing.T) {
	for _, optional := range []bool{false, true} {
		name := "required"
		if optional {
			name = "optional"
		}
		t.Run(name, func(t *testing.T) {
			var loads atomic.Int32
			registry, guard, policy := authSetup(t, "bearer", &loads)
			transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
			if err != nil {
				t.Fatal(err)
			}
			var origins []attribution.Origin
			check := func(ctx context.Context, subject value.Optional[authAccount]) (foundryhttp.NoContent, error) {
				origin := attribution.FromContext(ctx)
				origins = append(origins, origin)
				member, present := subject.Get()
				identity, attributed := origin.Model()
				if present {
					expected, _ := member.FoundryIdentity()
					if !attributed || identity != expected || origin.Guard() != attribution.GuardName(guard.Name()) {
						t.Error("verified handler lacks identity")
					}
					if err := policy.Authorize(ctx, guard, member.ID); err != nil {
						return foundryhttp.NoContent{}, err
					}
				} else if attributed || origin.System() != "" || origin.Guard() != "" {
					t.Error("anonymous request inherited attribution")
				}
				metadata := origin.Request()
				if metadata.ID == "" || metadata.ID == "forged" || metadata.ID != foundryhttp.RequestID(ctx) || metadata.IP != netip.MustParseAddr("192.0.2.7") || metadata.UserAgent != "fixture" {
					t.Error("lost trusted request metadata")
				}
				return foundryhttp.NoContent{}, nil
			}
			var registration foundryhttp.RouteRegistration
			if optional {
				registration = foundryhttp.OptionalAuthentication(authEndpoint(foundryhttp.Public), transport, guard).Handle(func(ctx context.Context, subject value.Optional[authAccount], _ authInput) (foundryhttp.NoContent, error) {
					return check(ctx, subject)
				})
			} else {
				registration = foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(ctx context.Context, member authAccount, _ authInput) (foundryhttp.NoContent, error) {
					return check(ctx, value.Set(member))
				})
			}
			router := newAuthRouter(t, registration)
			for index, credential := range []string{"valid", "other", "", "invalid", "pending", "disabled"} {
				request := httptest.NewRequest("GET", "/profile", nil)
				request.RemoteAddr = "192.0.2.7:1234"
				origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: attribution.RequestID("fixture-" + strconv.Itoa(index)), IP: netip.MustParseAddr("192.0.2.7"), UserAgent: "fixture"})
				if err != nil {
					t.Fatal(err)
				}
				origin, err = origin.WithModel(authAccount{ID: 99, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				origin, err = origin.WithGuard("outer.guard")
				if err != nil {
					t.Fatal(err)
				}
				parent, err := attribution.WithContext(request.Context(), origin)
				if err != nil {
					t.Fatal(err)
				}
				request = request.WithContext(parent)

				request.Header.Set("User-Agent", "fixture")
				request.Header.Set(foundryhttp.RequestIDHeader, "forged")
				if credential != "" {
					request.Header.Set("Authorization", "Bearer "+credential)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				accepted := credential == "valid" || credential == "other" || (optional && credential == "")
				if accepted && response.Code != 204 {
					t.Fatal("valid handler rejected", response.Code, response.Body.String())
				}
				if !accepted && response.Code == 204 {
					t.Fatal("unverified request entered handler")
				}
			}
			count := 2
			if optional {
				count = 3
			}
			if len(origins) != count || loads.Load() != 3 {
				t.Fatal("attribution changed guard hydration", len(origins), loads.Load())
			}
			if origins[0].Request().ID == origins[1].Request().ID {
				t.Fatal("origin shared across requests")
			}
		})
	}
}
