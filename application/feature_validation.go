package application

import (
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func prepareFeatureSettings(s Settings, source clock.Clock) (FeatureSettings, error) {
	f := s.Features
	for _, target := range persistenceTargets(&f) {
		if *target.connection == "" {
			*target.connection = s.Services.Database.Default
		}
		if _, ok := s.Services.Database.Connections[*target.connection]; !ok {
			return f, fault.New(fault.Missing, "feature database is not configured")
		}
		if !sqlname.Valid(*target.schema) {
			return f, fault.New(fault.Invalid, "feature requires a valid PostgreSQL schema")
		}
	}
	if f.Auth.Sessions.Enabled || f.Auth.Tokens.Enabled {
		if err := f.Auth.Registry.Validate(); err != nil {
			return f, err
		}
	}
	if f.Auth.Sessions.Enabled {
		c := &f.Auth.Sessions
		if c.Config.Namespace == (keyspace.Namespace{}) {
			c.Config.Namespace = s.Services.Namespace
			c.Config.Namespace.Application += ".auth.sessions"
		}
		for _, err := range []error{c.Config.Validate(), (sessionpg.Config{Schema: c.Schema, Clock: source}).Validate()} {
			if err != nil {
				return f, err
			}
		}
		if len(f.Auth.Browser.Guards) < 1 || len(f.Auth.Browser.Guards) > 128 {
			return f, fault.New(fault.Invalid, "browser auth requires bounded named guard policies")
		}
		if _, ok := f.Auth.Browser.Guards[f.Auth.Browser.Default]; !ok {
			return f, fault.New(fault.Missing, "default browser guard is not configured")
		}
		cookies := make(map[string]bool)
		for name, policy := range f.Auth.Browser.Guards {
			if err := namedservice.Validate(string(name)); err != nil {
				return f, err
			}
			if err := policy.runtime(source).Validate(); err != nil {
				return f, err
			}
			// Two browser actors must not overwrite one another's credentials.
			id := string(policy.Cookie) + "\x00" + policy.Options.Domain + "\x00" + policy.Options.Path
			if cookies[id] {
				return f, fault.New(fault.Duplicate, "browser guard policies share a cookie scope")
			}
			cookies[id] = true
		}
	}
	if err := prepareMFA(&f.Auth.MFA, s, source); err != nil {
		return f, err
	}
	if f.Auth.Tokens.Enabled {
		c := &f.Auth.Tokens
		if c.Config.Namespace == (keyspace.Namespace{}) {
			c.Config.Namespace = s.Services.Namespace
			c.Config.Namespace.Application += ".auth.tokens"
		}
		for _, err := range []error{c.Config.Validate(), (tokenpg.Config{Schema: c.Schema, Clock: source}).Validate(), namedservice.Validate(string(f.Auth.DefaultTokenGuard))} {
			if err != nil {
				return f, err
			}
		}
	}
	if f.Idempotency.Enabled {
		if err := f.Idempotency.Config.Validate(); err != nil {
			return f, err
		}
	}
	if f.Events.Enabled {
		if err := f.Events.Config.Validate(); err != nil {
			return f, err
		}
	}
	if f.Extensions.Enabled {
		c := f.Extensions
		if err := (extensions.Config{Schema: c.Schema, Clock: source, MaxActive: c.MaxActive, Timeout: c.Timeout}).Validate(); err != nil {
			return f, err
		}
		f.Extensions = c
	}
	if f.Notifications.Enabled {
		c := f.Notifications
		if err := (notifications.Config{Schema: c.Schema, Clock: source, MaxActive: c.MaxActive, Timeout: c.Timeout}).Validate(); err != nil {
			return f, err
		}
		f.Notifications = c
	}
	if f.Audit.Enabled {
		if err := f.Audit.Config.Validate(); err != nil {
			return f, err
		}
	}
	if f.Outbox.Enabled {
		if err := f.Outbox.runtime(source).Validate(); err != nil {
			return f, err
		}
		if len(f.Outbox.Jobs) > 128 {
			return f, fault.New(fault.Invalid, "too many configured job outboxes")
		}
		for name, destination := range f.Outbox.Jobs {
			if _, ok := s.Services.Jobs.Connections[name]; !ok {
				return f, fault.New(fault.Missing, "outbox job connection is not configured")
			}
			if err := destination.Validate(); err != nil {
				return f, err
			}
		}
	}
	if f.Locales.Enabled {
		locales, err := i18n.NewLocaleSet(f.Locales.Default, f.Locales.Locales...)
		if err != nil {
			return f, err
		}
		if f.Locales.Fallback != "" && !locales.Contains(f.Locales.Fallback) {
			return f, fault.New(fault.Missing, "locale fallback is not configured")
		}
	}
	if f.Attachments.Enabled {
		if !f.Extensions.Enabled || len(s.Services.Storage.Disks) == 0 {
			return f, fault.New(fault.Missing, "attachments require model extensions and storage")
		}
		if err := f.Attachments.Config.Validate(); err != nil {
			return f, err
		}
	}
	if f.Reports.Enabled {
		if f.Reports.Config.TimeZone == "" {
			f.Reports.Config.TimeZone = s.TimeZone
		}
		if err := f.Reports.Config.Validate(); err != nil {
			return f, err
		}
	}
	if f.Health.Enabled {
		if err := f.Health.Config.Validate(); err != nil {
			return f, err
		}
	}
	return f, nil
}
