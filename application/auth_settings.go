package application

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type SessionSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   session.Config
}
type TokenSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   token.Config
}

// MFASettings configures the application MFA store. It requires application
// encryption keys; factor secrets are encrypted with the application key ring.
// Schema holds the factor table and must also resolve the domain models that
// factor callbacks lock. Config.Issuer defaults to the application name.
type MFASettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   mfa.Config
}

//foundry:config
type BrowserGuardSettings struct {
	Cookie  http.CookieName
	Options http.CookieOptions
	CSRF    http.CSRFConfig
}

func DefaultBrowserGuardSettings(name auth.GuardName) BrowserGuardSettings {
	c := http.DefaultBrowserSessionConfig()
	c.Cookie = http.CookieName("__Host-foundry_" + string(name))
	return BrowserGuardSettings{Cookie: c.Cookie, Options: c.Options, CSRF: c.CSRF}
}
func (s BrowserGuardSettings) runtime(source clock.Clock) http.BrowserSessionConfig {
	return http.BrowserSessionConfig{Cookie: s.Cookie, Options: s.Options, CSRF: s.CSRF, Clock: source}
}

type BrowserGuards map[auth.GuardName]BrowserGuardSettings

func (m *BrowserGuards) UnmarshalText(data []byte) error {
	schema, err := BrowserGuardSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, DefaultBrowserGuardSettings, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type BrowserSettings struct {
	Default auth.GuardName
	Guards  BrowserGuards
}
type AuthSettings struct {
	Registry          auth.Config
	Sessions          SessionSettings
	Tokens            TokenSettings
	MFA               MFASettings
	Browser           BrowserSettings
	DefaultTokenGuard auth.GuardName
}

func DefaultAuthSettings() AuthSettings {
	return AuthSettings{Registry: auth.DefaultConfig(), Sessions: SessionSettings{Schema: sessionpg.DefaultConfig().Schema, Config: session.DefaultConfig(keyspace.Namespace{})}, Tokens: TokenSettings{Schema: tokenpg.DefaultConfig().Schema, Config: token.DefaultConfig(keyspace.Namespace{})}, MFA: MFASettings{Schema: mfapg.DefaultConfig().Schema, Config: mfa.DefaultConfig(keyspace.Namespace{}, "")}, Browser: BrowserSettings{Default: "web", Guards: BrowserGuards{"web": DefaultBrowserGuardSettings("web")}}, DefaultTokenGuard: "api"}
}

// prepareMFA fills MFA defaults from the application and validates that its
// factor secrets can be encrypted, before any resource is acquired.
func prepareMFA(c *MFASettings, s Settings, source clock.Clock) error {
	if !c.Enabled {
		return nil
	}
	if !s.Encryption.Enabled() {
		return fault.New(fault.Missing, "MFA requires application encryption keys (Encryption.KeyID and Encryption.Key)")
	}
	if c.Config.Namespace == (keyspace.Namespace{}) {
		c.Config.Namespace = s.Services.Namespace
		c.Config.Namespace.Application += ".auth.mfa"
	}
	if c.Config.Issuer == "" {
		c.Config.Issuer = s.Services.Namespace.Application
	}
	for _, err := range []error{c.Config.Validate(), (mfapg.Config{Schema: c.Schema, Clock: source}).Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
