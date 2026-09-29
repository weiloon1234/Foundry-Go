package auth_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"testing/synctest"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type policyInspectionFailure struct {
	inspect func()
	cycle   bool
}

func (*policyInspectionFailure) Error() string { return "private policy failure" }
func (e *policyInspectionFailure) As(any) bool { e.inspect(); return false }
func (e *policyInspectionFailure) Unwrap() error {
	if e.cycle {
		return e
	}
	return nil
}

func TestPolicyErrorInspectionStaysInScope(t *testing.T) {
	for _, mode := range []string{"policy", "before", "after", "guest"} {
		for _, behavior := range []string{"panic", "goexit", "cycle", "wrapped denial"} {
			t.Run(mode+"/"+behavior, func(t *testing.T) {
				failure := error(&policyInspectionFailure{inspect: func() {}, cycle: behavior == "cycle"})
				switch behavior {
				case "panic":
					failure = &policyInspectionFailure{inspect: func() { panic("private") }}
				case "goexit":
					failure = &policyInspectionFailure{inspect: runtime.Goexit}
				case "wrapped denial":
					failure = fmt.Errorf("wrapped: %w", auth.NewDenial("documents.closed", "Closed"))
				}
				p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
					return value.Set(account{ID: 7, Enabled: true}), nil
				})
				g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
				policy := auth.DefinePolicy("documents.inspect", func(context.Context, account, document) (bool, error) {
					if mode == "policy" {
						return false, failure
					}
					return true, nil
				})
				guest := auth.DefineGuestPolicy("documents.guest", func(context.Context, value.Optional[account], document) (bool, error) { return false, failure })
				before := auth.DefineBefore("documents.before", func(context.Context, account, auth.PolicyName) (auth.Verdict, error) {
					if mode == "before" {
						return auth.Abstain, failure
					}
					return auth.Abstain, nil
				})
				after := auth.DefineAfter("documents.after", func(context.Context, account, auth.PolicyName) (auth.Verdict, error) {
					if mode == "after" {
						return auth.Abstain, failure
					}
					return auth.Abstain, nil
				})
				r := registry(t, g.Registration(), policy.Registration(), guest.Registration(), before.Registration(), after.Registration())
				var credentials []auth.Credential
				if mode != "guest" {
					credentials = append(credentials, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
				}
				s := scope(t, r, credentials...)
				returned := false
				var decision auth.Decision
				var err error
				escaped := callback.Isolated("policy caller", func() error {
					if mode == "guest" {
						decision, err = guest.Inspect(s.Context(), g, document{})
					} else {
						decision, err = policy.Inspect(s.Context(), g, document{})
					}
					returned = true
					return nil
				})
				if escaped != nil || !returned || decision.Allowed() {
					t.Fatal("policy inspection escaped or granted access")
				}
				if behavior == "wrapped denial" {
					if denial, ok := decision.Denial(); err != nil || !ok || denial.Code() != "documents.closed" {
						t.Fatal("wrapped denial lost")
					}
				} else if err == nil {
					t.Fatal("failed inspection was accepted")
				}
				failure = nil
				if mode == "guest" {
					_, err = guest.Inspect(s.Context(), g, document{})
				} else {
					_, err = policy.Inspect(s.Context(), g, document{})
				}
				if err != nil {
					t.Fatal("subsequent policy operation failed")
				}
			})
		}
	}
}

func TestPolicyInspectionRetainsScopeUntilExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
			return value.Set(account{ID: 7, Enabled: true}), nil
		})
		g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
		policy := auth.DefinePolicy("documents.block", func(context.Context, account, document) (bool, error) {
			return false, &policyInspectionFailure{inspect: func() { close(entered); <-release }}
		})
		s := scope(t, registry(t, g.Registration(), policy.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
		done := make(chan struct{})
		go func() { defer close(done); _, _ = policy.Inspect(s.Context(), g, document{}) }()
		<-entered
		closed := make(chan struct{})
		go func() { defer close(closed); _ = s.Close() }()
		synctest.Wait()
		select {
		case <-closed:
			t.Error("scope released a running error inspector")
		default:
		}
		close(release)
		<-done
		<-closed
	})
}
