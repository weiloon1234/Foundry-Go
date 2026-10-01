package application

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	mfacommand "github.com/weiloon1234/Foundry-Go/auth/mfa/command"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
)

const SessionProvider foundation.ProviderID = "foundry.application.sessions"
const TokenProvider foundation.ProviderID = "foundry.application.tokens"
const MFAProvider foundation.ProviderID = "foundry.application.mfa"

var MFAKey = foundation.NewKey[*mfa.Store](string(MFAProvider))

// AuthorizationProvider owns the application's shared authorization registry.
const AuthorizationProvider foundation.ProviderID = "foundry.application.authorization"

var SessionKey = foundation.NewKey[*session.Store](string(SessionProvider))
var TokenKey = foundation.NewKey[*token.Store](string(TokenProvider))

// AuthorizationKey is the application-owned registry of contributed guards,
// policies, permissions and hooks. Plugins and providers contribute with
// auth.RegisterAuthorization(r, AuthorizationKey, declaration) or Authorization.
// Configured browser/token guards extend it, so every policy and permission
// registered here is available to their routes. Its Config is Features.Auth.Registry.
var AuthorizationKey = foundation.NewKey[*auth.Registry](string(AuthorizationProvider))

func registerAuth(builder *foundation.Builder, s AuthSettings, source clock.Clock) {
	builder.Register(foundation.Module{Name: AuthorizationProvider, OnRegister: func(r *foundation.Registrar) error {
		return auth.RegisterRegistry(r, AuthorizationKey, s.Registry)
	}})
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
	if s.MFA.Enabled {
		c := s.MFA
		builder.Register(foundation.Module{Name: MFAProvider, Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(c.Database), EncryptionProvider}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, MFAKey, func(r foundation.Resolver) (*mfa.Store, error) {
				db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
				if err != nil {
					return nil, err
				}
				keys, err := foundation.Resolve(r, EncryptionKey)
				if err != nil {
					return nil, err
				}
				backend, err := mfapg.New(db, mfapg.Config{Schema: c.Schema, Clock: source})
				if err != nil {
					return nil, err
				}
				return mfa.NewStore(backend, keys, c.Config)
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

// MFA returns the configured MFA store, whose factor secrets use the
// application key ring. Bind model factors with mfa.New(store, provider, ...).
func (s Services) MFA() (*mfa.Store, error) { return Resolve(s, MFAKey) }

// MFACommand declares the `mfa reencrypt` operator command. After adding a new
// active key (keeping the old one in Encryption.Previous), it re-encrypts every
// stored factor under the active key in bounded batches without any user's
// password. Remove the previous key only after it reports complete with no
// failures. Register it in the application's CLI registry.
func MFACommand() (cli.Declaration, error) {
	return mfacommand.Declaration(func(r foundation.Resolver) (*mfa.Store, error) {
		return foundation.Resolve(r, MFAKey)
	})
}

func (s Services) Sessions() (*session.Store, error) { return Resolve(s, SessionKey) }
func (s Services) Tokens() (*token.Store, error)     { return Resolve(s, TokenKey) }

// Authorization returns the application-owned registry shared by configured
// guards. It contains every contributed guard, policy, permission and hook.
func (s Services) Authorization() (*auth.Registry, error) { return Resolve(s, AuthorizationKey) }

// authorizationDeclaration is any typed guard, policy, permission or hook.
type authorizationDeclaration interface {
	Registration() auth.Registration
	Validate() error
}

// AuthorizationDeclaration is one typed contribution to AuthorizationKey. Its
// concrete declaration type is retained for foundation override validation.
type AuthorizationDeclaration struct {
	register func(*foundation.Registrar) error
}

// Authorize wraps a typed guard, policy, permission or Before/After hook for
// Authorization. Construction performs no validation; registration does.
func Authorize[D authorizationDeclaration](declaration D) AuthorizationDeclaration {
	return AuthorizationDeclaration{register: func(r *foundation.Registrar) error {
		return auth.RegisterAuthorization(r, AuthorizationKey, declaration)
	}}
}

// Authorization returns a provider contributing application policies and
// permissions to the shared registry, for example
// application.New(s).Register(application.Authorization("app.authorization",
// application.Authorize(ViewAccount), application.Authorize(ReadOrder))).
func Authorization(id foundation.ProviderID, declarations ...AuthorizationDeclaration) foundation.Provider {
	return foundation.Module{Name: id, Requires: []foundation.ProviderID{AuthorizationProvider}, OnRegister: func(r *foundation.Registrar) error {
		if len(declarations) == 0 {
			return fault.New(fault.Invalid, "application authorization requires declarations")
		}
		for _, declaration := range declarations {
			if declaration.register == nil {
				return fault.New(fault.Invalid, "invalid application authorization declaration")
			}
			if err := declaration.register(r); err != nil {
				return err
			}
		}
		return nil
	}}
}

// guardRegistry derives an immutable registry from the application registry
// that also contains one configured guard. It shares the application's
// callback capacity and sees every contributed policy, permission and hook.
func guardRegistry(s Services, guard auth.Registration) (*auth.Registry, error) {
	shared, err := s.Authorization()
	if err != nil {
		return nil, err
	}
	return shared.With(guard)
}

// BrowserGuard retains one concrete actor across session issuance, browser policy
// and its route-group default. Authentication is distinct from resource policy.
// Its registry extends the application's AuthorizationKey registry, so routes
// can use WithPermissions and handlers can authorize with contributed policies.
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
	registry, err := guardRegistry(s, sessions.Guard().Registration())
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

// NewTokenGuard binds a token guard to the configured token store; options such
// as token.WithLifetimes apply to this guard only.
func NewTokenGuard[M model.Identifiable, K any](s Services, name auth.GuardName, provider auth.Provider[M, K], source auth.CredentialName, allowed auth.AccessScopes[M], opts ...token.Option) (TokenGuard[M, K], error) {
	var result TokenGuard[M, K]
	if name == "" {
		name = s.features.Auth.DefaultTokenGuard
	}
	store, err := s.Tokens()
	if err != nil {
		return result, err
	}
	tokens, err := token.New(store, name, provider, source, allowed, opts...)
	if err != nil {
		return result, err
	}
	registry, err := guardRegistry(s, tokens.Guard().Registration())
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
