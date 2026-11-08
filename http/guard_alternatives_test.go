package http_test

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func alternativeScopes(t *testing.T, names ...auth.AccessScopeName) auth.AccessScopes[authAccount] {
	t.Helper()
	declared := make([]auth.AccessScope[authAccount], len(names))
	for i, name := range names {
		declared[i] = auth.DefineAccessScope[authAccount](name)
	}
	result, err := auth.NewAccessScopes(declared...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func alternativeBinding(t *testing.T, grants auth.AccessScopes[authAccount], registrations ...auth.Registration) (h.GuardBinding[authAccount], *atomic.Int32) {
	t.Helper()
	loads := new(atomic.Int32)
	provider := auth.DefineProvider("accounts", authAccount{}.reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		loads.Add(1)
		if id == 3 {
			return value.Optional[authAccount]{}, nil
		}
		if id == 5 {
			return value.Optional[authAccount]{}, auth.Forbidden.WithCause(errors.New("private authority failure"))
		}
		return value.Set(authAccount{ID: id, Enabled: id != 4}), nil
	}, func(_ context.Context, actor authAccount) (bool, error) { return actor.Enabled, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, raw secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
		id, assurance := int64(1), auth.Authenticated
		scopes := grants
		switch raw.Reveal() {
		case "valid", "unscoped":
		case "pending":
			assurance = auth.PendingMFA
		case "deleted":
			id = 3
		case "disabled":
			id = 4
		case "authority":
			id = 5
		case "empty":
			scopes = auth.AccessScopes[authAccount]{}
		default:
			return value.Optional[auth.Proof[authAccount, int64]]{}, nil
		}
		proof, err := auth.NewScopedProof(authAccount{ID: id}.reference(), assurance, scopes)
		if raw.Reveal() == "unscoped" {
			proof, err = auth.NewProof(authAccount{ID: id}.reference(), assurance)
		}
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("api", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), append([]auth.Registration{guard.Registration()}, registrations...)...)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	return binding, loads
}

func TestAnyOfRequiresOneCompleteBranchBeforeInputAndResourceWork(t *testing.T) {
	for _, grants := range [][]auth.AccessScopeName{nil, {"editorial.write"}, {"editorial.approve"}, {"editorial.write", "editorial.approve"}} {
		for mask := 0; mask < 4; mask++ {
			t.Run(fmt.Sprintf("%v/permissions-%d", grants, mask), func(t *testing.T) {
				writeAllowed, approveAllowed := mask&1 != 0, mask&2 != 0
				var calls []string
				permission := func(name auth.PermissionName, allow *bool) auth.Permission[authAccount] {
					return auth.DefinePermission(name, func(ctx context.Context, actor authAccount) (bool, error) {
						calls = append(calls, string(name))
						if actor.ID != 1 || attribution.FromContext(ctx).Guard() != "api" {
							t.Error("permission lost authenticated actor attribution")
						}
						return *allow, nil
					})
				}
				write := permission("editorial.write", &writeAllowed)
				approve := permission("editorial.approve", &approveAllowed)
				binding, loads := alternativeBinding(t, alternativeScopes(t, grants...), write.Registration(), approve.Registration())
				options := []h.GuardAlternative[authAccount]{
					h.AllOf(h.Scopes(alternativeScopes(t, "editorial.write")), h.Permissions(write)),
					h.AllOf(h.Scopes(alternativeScopes(t, "editorial.approve")), h.Permissions(approve)),
				}
				access := h.AnyOf(options...)
				options[0] = h.GuardAlternative[authAccount]{}
				actorCalls, handled := 0, 0
				actor := h.DefineMiddleware("test.alternative.actor", func(next stdhttp.Handler) (stdhttp.Handler, error) {
					return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
						actorCalls++
						next.ServeHTTP(w, r)
					}), nil
				})
				endpoint := h.Authenticated(authEndpoint(h.Guarded), binding, access, h.ActorMiddleware[authAccount](actor))
				resourceAllowed := true
				router := newAuthRouter(t, endpoint.Handle(func(ctx context.Context, _ authAccount, _ authInput) (h.NoContent, error) {
					handled++
					matched, _ := h.MatchedRoute(ctx)
					matched.Authentication.AnyOf[0].RequiredScopes[0] = "mutated"
					if !resourceAllowed {
						return h.NoContent{}, auth.Forbidden
					}
					return h.NoContent{}, nil
				}))
				want := 403
				if slices.Contains(grants, "editorial.write") && writeAllowed || slices.Contains(grants, "editorial.approve") && approveAllowed {
					want = 204
				}
				if response := authServe(router, []string{"Bearer valid"}); response.Code != want {
					t.Fatal("branch truth table", response.Code, want)
				}
				if loads.Load() != 1 || actorCalls != handled || (want == 403 && handled != 0) {
					t.Fatal("admission ownership/order", loads.Load(), actorCalls, handled)
				}
				var expected []string
				if slices.Contains(grants, "editorial.write") {
					expected = append(expected, "editorial.write")
				}
				if !(slices.Contains(grants, "editorial.write") && writeAllowed) && slices.Contains(grants, "editorial.approve") {
					expected = append(expected, "editorial.approve")
				}
				if !slices.Equal(calls, expected) {
					t.Fatal("scope gate or short circuit", calls, expected)
				}
				info, err := h.InspectAuthenticated(endpoint)
				if err != nil || len(info.Route.Authentication.AnyOf) != 2 || info.Route.Authentication.RequiredPermissions != nil {
					t.Fatal("grouped inspection", err)
				}
				info.Route.Authentication.AnyOf[0].RequiredScopes[0] = "changed"
				info.Route.Authentication.AnyOf[1].RequiredPermissions[0] = "changed"
				routes := router.Routes()
				routes[0].Authentication.AnyOf[0] = h.GuardAlternativeInfo{}
				if router.Routes()[0].Authentication.AnyOf[0].RequiredScopes[0] != "editorial.write" {
					t.Fatal("mutable requirements")
				}
				resourceAllowed = false
				if response := authServe(router, []string{"Bearer valid"}); response.Code != 403 {
					t.Fatal("route guard bypassed resource authorization")
				}
				writeAllowed, approveAllowed = false, false
				before := handled
				request := httptest.NewRequest("GET", "/profile?bad=%zz", nil)
				request.Header.Set("Authorization", "Bearer valid")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != 403 || handled != before {
					t.Fatal("permission revocation or pre-decode denial lost")
				}
			})
		}
	}
}

func TestAnyOfAuthenticationAndExplicitScopeFreeBranches(t *testing.T) {
	permissionCalls := 0
	permission := auth.DefinePermission("editorial.write", func(context.Context, authAccount) (bool, error) {
		permissionCalls++
		return true, nil
	})
	required := alternativeScopes(t, "editorial.write")
	binding, _ := alternativeBinding(t, required, permission.Registration())
	for _, scoped := range []bool{true, false} {
		branch := h.AllOf(h.Permissions(permission))
		if scoped {
			branch = h.AllOf(h.Scopes(required), h.Permissions(permission))
		}
		router := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding, h.AnyOf(branch)).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
		for _, token := range []string{"", "invalid", "revoked", "deleted", "disabled", "pending", "authority", "unscoped", "empty", "valid"} {
			before := permissionCalls
			allowed := token == "valid" || !scoped && (token == "unscoped" || token == "empty")
			response := authServe(router, []string{"Bearer " + token})
			if (response.Code == 204) != allowed || (!allowed && permissionCalls != before) {
				t.Fatal("authentication/scope boundary", token, scoped, response.Code)
			}
		}
	}
	before := permissionCalls
	scopeOnly := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding, h.AnyOf(h.AllOf(h.Scopes(required)))).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
	if response := authServe(scopeOnly, []string{"Bearer valid"}); response.Code != 204 || permissionCalls != before {
		t.Fatal("explicit scope-only declaration changed")
	}
}

func TestAnyOfOnlyNormalDenialPermitsFallback(t *testing.T) {
	for _, mode := range []string{"false", "denial", "wrapped denial", "failure", "forbidden", "joined", "reverse joined", "joined denials", "classified", "partial", "panic", "goexit", "cancel", "deadline", "overload", "first success"} {
		t.Run(mode, func(t *testing.T) {
			first := auth.DefinePermission("first", func(context.Context, authAccount) (bool, error) {
				denial := auth.NewDenial("closed", "private decision")
				failure := errors.New("private authority failure")
				switch mode {
				case "false":
					return false, nil
				case "denial":
					return false, denial
				case "wrapped denial":
					return false, fmt.Errorf("private: %w", denial)
				case "failure":
					return true, failure
				case "forbidden":
					return true, auth.Forbidden.WithCause(failure)
				case "joined":
					return true, errors.Join(denial, failure)
				case "reverse joined":
					return true, errors.Join(failure, denial)
				case "joined denials":
					return true, errors.Join(denial, denial)
				case "classified":
					return true, fault.Wrap(fault.Internal, "private", denial)
				case "partial":
					for range 70 {
						denial = fmt.Errorf("wrapped: %w", denial)
					}
					return true, denial
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					return true, context.Canceled
				case "deadline":
					return true, context.DeadlineExceeded
				case "overload":
					return true, fault.Overloaded
				}
				return true, nil
			})
			laterCalls := 0
			later := auth.DefinePermission("later", func(context.Context, authAccount) (bool, error) {
				laterCalls++
				if mode == "first success" {
					return false, errors.New("must not execute")
				}
				return true, nil
			})
			binding, _ := alternativeBinding(t, auth.AccessScopes[authAccount]{}, first.Registration(), later.Registration())
			router := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding, h.AnyOf(h.AllOf(h.Permissions(first)), h.AllOf(h.Permissions(later)))).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
			response := authServe(router, []string{"Bearer valid"})
			fallback := mode == "false" || mode == "denial" || mode == "wrapped denial"
			if (response.Code == 204) != (fallback || mode == "first success") || (laterCalls == 1) != fallback {
				t.Fatal("failure became fallback", response.Code, laterCalls)
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatal("authority diagnostic exposed")
			}
		})
	}
}

func TestAnyOfRejectsInvalidDeclarationsBeforeRegistration(t *testing.T) {
	p := auth.DefinePermission("editorial.write", func(context.Context, authAccount) (bool, error) { return true, nil })
	q := auth.DefinePermission("editorial.approve", func(context.Context, authAccount) (bool, error) { return true, nil })
	scopes := alternativeScopes(t, "editorial.write")
	binding, _ := alternativeBinding(t, scopes, p.Registration(), q.Registration())
	branch := h.AllOf(h.Scopes(scopes), h.Permissions(p, q))
	valid := h.AnyOf(branch)
	impostor := auth.DefinePermission(p.Name(), func(context.Context, authAccount) (bool, error) { return true, nil })
	cases := map[string][]h.GuardOption[authAccount]{
		"empty group":              {h.AnyOf[authAccount]()},
		"zero branch":              {h.AnyOf(h.GuardAlternative[authAccount]{})},
		"empty branch":             {h.AnyOf(h.AllOf[authAccount]())},
		"nil requirement":          {h.AnyOf(h.AllOf[authAccount](nil))},
		"empty scopes":             {h.AnyOf(h.AllOf(h.Scopes(auth.AccessScopes[authAccount]{}), h.Permissions(p)))},
		"empty permissions":        {h.AnyOf(h.AllOf(h.Scopes(scopes), h.Permissions[authAccount]()))},
		"repeated scopes":          {h.AnyOf(h.AllOf(h.Scopes(scopes), h.Scopes(scopes)))},
		"repeated permissions":     {h.AnyOf(h.AllOf(h.Permissions(p), h.Permissions(q)))},
		"duplicate permissions":    {h.AnyOf(h.AllOf(h.Permissions(p, p)))},
		"duplicate branch":         {h.AnyOf(branch, branch)},
		"permuted branch":          {h.AnyOf(branch, h.AllOf(h.Permissions(q, p), h.Scopes(scopes)))},
		"repeated group":           {valid, valid},
		"mixed scopes first":       {h.Scopes(scopes), valid},
		"mixed scopes last":        {valid, h.Scopes(scopes)},
		"mixed permissions first":  {h.Permissions(p), valid},
		"mixed permissions last":   {valid, h.Permissions(p)},
		"impostor in later branch": {h.AnyOf(branch, h.AllOf(h.Permissions(impostor)))},
		"undefined permission":     {h.AnyOf(h.AllOf(h.Permissions(auth.Permission[authAccount]{})))},
		"too many branches":        {h.AnyOf(make([]h.GuardAlternative[authAccount], h.MaxGuardAlternatives+1)...)},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			endpoint := h.Authenticated(authEndpoint(h.Guarded), binding, options...)
			if err := h.ValidateAuthenticated(endpoint); err == nil {
				t.Fatal("invalid declaration accepted")
			}
			if _, err := h.NewRouter(endpoint.Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) {
				t.Error("invalid route executed")
				return h.NoContent{}, nil
			})); err == nil {
				t.Fatal("invalid registration accepted")
			}
		})
	}
	base := h.Authenticated(authEndpoint(h.Guarded), binding)
	derived := h.ConfigureAuthenticated(base, valid)
	if err := h.ValidateAuthenticated(base); err != nil {
		t.Fatal("copied endpoint mutated base", err)
	}
	if err := h.ValidateAuthenticated(derived); err != nil {
		t.Fatal(err)
	}
	if err := h.ValidateAuthenticated(h.ConfigureAuthenticated(derived, h.Scopes(scopes))); err == nil {
		t.Fatal("later configure bypassed exclusivity")
	}
	firstError := h.AnyOf(h.AllOf(h.Scopes(scopes), h.Scopes(scopes), nil))
	if err := h.ValidateAuthenticated(h.Authenticated(authEndpoint(h.Guarded), binding, firstError, h.AnyOf[authAccount]())); !errors.Is(err, fault.Duplicate) {
		t.Fatal("later invalid options replaced the first declaration error", err)
	}
}

func TestAnyOfConjunctionAndRepeatedPermissionsStayFresh(t *testing.T) {
	for _, grantNames := range [][]auth.AccessScopeName{{"a"}, {"a", "b"}} {
		for _, lastAllowed := range []bool{false, true} {
			calls := 0
			shared := auth.DefinePermission("shared", func(context.Context, authAccount) (bool, error) {
				calls++
				return calls > 1, nil
			})
			last := auth.DefinePermission("last", func(context.Context, authAccount) (bool, error) { return lastAllowed, nil })
			skipped := auth.DefinePermission("skipped", func(context.Context, authAccount) (bool, error) {
				t.Error("a denied branch continued evaluating permissions")
				return true, nil
			})
			binding, _ := alternativeBinding(t, alternativeScopes(t, grantNames...), shared.Registration(), last.Registration(), skipped.Registration())
			access := h.AnyOf(
				h.AllOf(h.Scopes(alternativeScopes(t, "a")), h.Permissions(shared, skipped)),
				h.AllOf(h.Scopes(alternativeScopes(t, "a", "b")), h.Permissions(shared, last)),
			)
			router := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding, access).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
			response := authServe(router, []string{"Bearer valid"})
			if (response.Code == 204) != (len(grantNames) == 2 && lastAllowed) || calls != len(grantNames) {
				t.Fatal("conjunction weakened or permission cached", grantNames, lastAllowed, response.Code, calls)
			}
		}
	}
}

func TestAnyOfHooksCannotOverrideScopesAndAfterCanVeto(t *testing.T) {
	callbacks, beforeCalls, afterCalls := 0, 0, 0
	check := func(context.Context, authAccount) (bool, error) { callbacks++; return false, nil }
	write, approve := auth.DefinePermission("write", check), auth.DefinePermission("approve", check)
	veto := false
	before := auth.DefineBefore("allow", func(context.Context, authAccount, auth.PolicyName) (auth.Verdict, error) {
		beforeCalls++
		return auth.Allow, nil
	})
	after := auth.DefineAfter("veto", func(context.Context, authAccount, auth.PolicyName) (auth.Verdict, error) {
		afterCalls++
		if veto {
			return auth.Deny, nil
		}
		return auth.Abstain, nil
	})
	binding, _ := alternativeBinding(t, alternativeScopes(t, "approve"), write.Registration(), approve.Registration(), before.Registration(), after.Registration())
	access := h.AnyOf(h.AllOf(h.Scopes(alternativeScopes(t, "write")), h.Permissions(write)), h.AllOf(h.Scopes(alternativeScopes(t, "approve")), h.Permissions(approve)))
	router := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding, access).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
	if response := authServe(router, []string{"Bearer valid"}); response.Code != 204 || callbacks != 0 || beforeCalls != 1 || afterCalls != 1 {
		t.Fatal("hook bypassed scope gate or callback decision", response.Code, callbacks, beforeCalls, afterCalls)
	}
	veto = true
	if response := authServe(router, []string{"Bearer valid"}); response.Code != 403 || callbacks != 0 || beforeCalls != 2 || afterCalls != 2 {
		t.Fatal("after veto lost", response.Code)
	}
}
