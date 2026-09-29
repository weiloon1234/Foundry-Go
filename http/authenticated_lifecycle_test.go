package http_test

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type actorForm struct{ Name string }
type actorFormRequest = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, actorForm]

func TestTypedActorRequestLifecycle(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			var loads atomic.Int32
			registry, guard, _ := authSetup(t, "bearer", &loads)
			transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
			if err != nil {
				t.Fatal(err)
			}
			access := foundryhttp.Guarded
			if optional {
				access = foundryhttp.Public
			}
			base := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "actor.form", Method: foundryhttp.POST, Access: access}, foundryhttp.StaticPath("/form")), foundryhttp.EmptyQuery(), foundryhttp.FormBody(foundryhttp.DefineQuery(foundryhttp.QueryParam("name", foundryhttp.StringQuery[string](), func(b *actorForm) *string { return &b.Name }))), foundryhttp.EmptyResponse(204)).WithBodyValidation(validation.DefineField("name", func(b actorForm) string { return b.Name }).Rules(validation.NonBlank[string]()))
			var stages []string
			prepare := func(_ context.Context, in actorFormRequest) (foundryhttp.NoQuery, actorForm, error) {
				stages = append(stages, "prepare")
				in.Body.Name = strings.TrimSpace(in.Body.Name)
				return in.Query, in.Body, nil
			}
			authorize := func(actor value.Optional[authAccount], in actorFormRequest) error {
				stages = append(stages, "authorize")
				if in.Body.Name == "deny" {
					return foundryhttp.Forbidden
				}
				if selected, ok := actor.Get(); ok && selected.ID != 1 {
					t.Error("wrong typed actor")
				}
				return nil
			}
			var registration foundryhttp.RouteRegistration
			if optional {
				endpoint := foundryhttp.OptionalAuthentication(base, transport, guard).WithPreparation(prepare)
				if endpoint.WithAuthorization(nil).Validate() == nil {
					t.Fatal("nil optional callback accepted")
				}
				registration = endpoint.WithAuthorization(func(_ context.Context, a value.Optional[authAccount], in actorFormRequest) error {
					return authorize(a, in)
				}).Handle(func(context.Context, value.Optional[authAccount], actorFormRequest) (foundryhttp.NoContent, error) {
					stages = append(stages, "handler")
					return foundryhttp.NoContent{}, nil
				})
			} else {
				endpoint := foundryhttp.RequireAuthentication(base, transport, guard).WithPreparation(prepare)
				if endpoint.WithAuthorization(nil).Validate() == nil {
					t.Fatal("nil actor callback accepted")
				}
				registration = endpoint.WithAuthorization(func(_ context.Context, a authAccount, in actorFormRequest) error { return authorize(value.Set(a), in) }).Handle(func(context.Context, authAccount, actorFormRequest) (foundryhttp.NoContent, error) {
					stages = append(stages, "handler")
					return foundryhttp.NoContent{}, nil
				})
			}
			router := newAuthRouter(t, registration)
			for _, tc := range []struct {
				token, body string
				status      int
				sequence    string
			}{
				{"valid", "name=++Jane++", 204, "prepare,authorize,handler"},
				{"valid", "name=+", 422, "prepare,authorize"}, // authorization precedes validation
				{"valid", "name=deny", 403, "prepare,authorize"},
				{"invalid", "malformed=%", 401, ""},
				{"", "name=Jane", map[bool]int{false: 401, true: 204}[optional], map[bool]string{false: "", true: "prepare,authorize,handler"}[optional]},
			} {
				stages = nil
				before := loads.Load()
				req := httptest.NewRequest("POST", "/form", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != tc.status || strings.Join(stages, ",") != tc.sequence {
					t.Fatalf("%s: %d %v %s", tc.token, res.Code, stages, res.Body.String())
				}
				if tc.token == "valid" && loads.Load()-before != 1 {
					t.Fatal("actor loaded more than once")
				}
			}
		})
	}
}

type lifecycleStaff struct {
	ID         int64
	Department string
}

func (a lifecycleStaff) reference() model.Reference[lifecycleStaff, int64] {
	return model.NewReference[lifecycleStaff]("lifecycle_staff", a.ID, codec.Signed[int64]())
}
func (a lifecycleStaff) FoundryIdentity() (model.Identity, error) { return a.reference().Identity() }
func TestTypedRequestAuthorizationMultipleGuards(t *testing.T) {
	var memberLoads, staffLoads atomic.Int32
	_, member, _ := authSetup(t, "bearer", &memberLoads)
	provider := auth.DefineProvider("staff", (lifecycleStaff{}).reference(), func(_ context.Context, id int64) (value.Optional[lifecycleStaff], error) {
		staffLoads.Add(1)
		return value.Set(lifecycleStaff{ID: id, Department: "operations"}), nil
	}, func(context.Context, lifecycleStaff) (bool, error) { return true, nil })
	proof, err := auth.NewProof((lifecycleStaff{ID: 8}).reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[lifecycleStaff, int64]], error) {
		if token.Reveal() != "staff-valid" {
			return value.Optional[auth.Proof[lifecycleStaff, int64]]{}, nil
		}
		return value.Set(proof), nil
	})
	staff := auth.DefineGuard("staff", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), member.Registration(), staff.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	memberCalls, staffCalls := 0, 0
	members := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded).Within(foundryhttp.DefineScope("/members", "members")), transport, member).WithAuthorization(func(_ context.Context, actor authAccount, _ authInput) error {
		memberCalls++
		if actor.ID != 1 || !actor.Enabled {
			t.Error("member actor changed")
		}
		return nil
	})
	staffRoute := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded).Within(foundryhttp.DefineScope("/staff", "staff")), transport, staff).WithAuthorization(func(_ context.Context, actor lifecycleStaff, _ authInput) error {
		staffCalls++
		if actor.ID != 8 || actor.Department != "operations" {
			t.Error("staff actor changed")
		}
		return nil
	})
	router, err := foundryhttp.NewRouter(members.Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}), staffRoute.Handle(func(context.Context, lifecycleStaff, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, token string
		status      int
	}{{"/members/profile", "valid", 204}, {"/staff/profile", "staff-valid", 204}, {"/members/profile", "staff-valid", 401}, {"/staff/profile", "valid", 401}} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatal(tc.path, res.Code, res.Body.String())
		}
	}
	if memberCalls != 1 || staffCalls != 1 || memberLoads.Load() != 1 || staffLoads.Load() != 1 {
		t.Fatal("guard ownership or request actor reuse failed")
	}
}
