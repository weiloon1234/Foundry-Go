package application

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
)

const SessionProvider foundation.ProviderID = "foundry.application.sessions"
const TokenProvider foundation.ProviderID = "foundry.application.tokens"

var SessionKey = foundation.NewKey[*session.Store](string(SessionProvider))
var TokenKey = foundation.NewKey[*token.Store](string(TokenProvider))

func registerAuth(builder *foundation.Builder, s AuthSettings, source clock.Clock) {
	if s.Sessions.Enabled {
		c := s.Sessions
		builder.Register(foundation.Module{Name: SessionProvider, Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(c.Database)}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, SessionKey, func(r foundation.Resolver) (*session.Store, error) {
				db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
				if err != nil {
					return nil, err
				}
				backend, err := sessionpg.New(db, sessionpg.Config{Schema: c.Schema, Clock: source})
				if err != nil {
					return nil, err
				}
				return session.NewStore(backend, c.Config)
			})
		}})
	}
	if s.Tokens.Enabled {
		c := s.Tokens
		builder.Register(foundation.Module{Name: TokenProvider, Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(c.Database)}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, TokenKey, func(r foundation.Resolver) (*token.Store, error) {
				db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
				if err != nil {
					return nil, err
				}
				backend, err := tokenpg.New(db, tokenpg.Config{Schema: c.Schema, Clock: source})
				if err != nil {
					return nil, err
				}
				return token.NewStore(backend, c.Config)
			})
		}})
	}
}
func (s Services) Sessions() (*session.Store, error) { return Resolve(s, SessionKey) }
func (s Services) Tokens() (*token.Store, error)     { return Resolve(s, TokenKey) }

// BrowserGuard retains one concrete actor across session issuance, browser policy
// and its route-group default. Authentication is distinct from resource policy.
type BrowserGuard[M model.Identifiable, K any] struct {
	Sessions *session.Sessions[M, K]
	Browser  *http.BrowserSessions[M, K]
	Binding  http.GuardBinding[M]
}

func NewBrowserGuard[M model.Identifiable, K any](s Services, name auth.GuardName, provider auth.Provider[M, K], source auth.CredentialName) (BrowserGuard[M, K], error) {
	var result BrowserGuard[M, K]
	if name == "" {
		name = s.features.Auth.Browser.Default
	}
	policy, ok := s.features.Auth.Browser.Guards[name]
	if !ok {
		return result, fault.New(fault.Missing, "browser guard policy is not configured")
	}
	store, err := s.Sessions()
	if err != nil {
		return result, err
	}
	sessions, err := session.New(store, name, provider, source)
	if err != nil {
		return result, err
	}
	registry, err := auth.NewRegistry(s.features.Auth.Registry, sessions.Guard().Registration())
	if err != nil {
		return result, err
	}
	browser, err := http.NewBrowserSessions(registry, sessions, policy.runtime(s.clock))
	if err != nil {
		return result, err
	}
	binding, err := http.BindGuard(browser.Authentication(), sessions.Guard())
	if err != nil {
		return result, err
	}
	return BrowserGuard[M, K]{sessions, browser, binding}, nil
}

type TokenGuard[M model.Identifiable, K any] struct {
	Tokens         *token.Tokens[M, K]
	Authentication *http.Authentication
	Binding        http.GuardBinding[M]
}

func NewTokenGuard[M model.Identifiable, K any](s Services, name auth.GuardName, provider auth.Provider[M, K], source auth.CredentialName, allowed auth.AccessScopes[M]) (TokenGuard[M, K], error) {
	var result TokenGuard[M, K]
	if name == "" {
		name = s.features.Auth.DefaultTokenGuard
	}
	store, err := s.Tokens()
	if err != nil {
		return result, err
	}
	tokens, err := token.New(store, name, provider, source, allowed)
	if err != nil {
		return result, err
	}
	registry, err := auth.NewRegistry(s.features.Auth.Registry, tokens.Guard().Registration())
	if err != nil {
		return result, err
	}
	transport, err := http.NewAuthentication(registry, http.BearerCredential(source))
	if err != nil {
		return result, err
	}
	binding, err := http.BindGuard(transport, tokens.Guard())
	if err != nil {
		return result, err
	}
	return TokenGuard[M, K]{tokens, transport, binding}, nil
}
