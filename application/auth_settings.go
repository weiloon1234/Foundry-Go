package application

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
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
	Browser           BrowserSettings
	DefaultTokenGuard auth.GuardName
}

func DefaultAuthSettings() AuthSettings {
	return AuthSettings{Registry: auth.DefaultConfig(), Sessions: SessionSettings{Schema: sessionpg.DefaultConfig().Schema, Config: session.DefaultConfig(keyspace.Namespace{})}, Tokens: TokenSettings{Schema: tokenpg.DefaultConfig().Schema, Config: token.DefaultConfig(keyspace.Namespace{})}, Browser: BrowserSettings{Default: "web", Guards: BrowserGuards{"web": DefaultBrowserGuardSettings("web")}}, DefaultTokenGuard: "api"}
}
