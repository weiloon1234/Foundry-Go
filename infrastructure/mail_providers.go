package infrastructure

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/cloudflare"
	"github.com/weiloon1234/Foundry-Go/email/failover"
	emaillog "github.com/weiloon1234/Foundry-Go/email/log"
	"github.com/weiloon1234/Foundry-Go/email/mailgun"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/email/postmark"
	"github.com/weiloon1234/Foundry-Go/email/resend"
	"github.com/weiloon1234/Foundry-Go/email/ses"
	"github.com/weiloon1234/Foundry-Go/email/smtp"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/storage"
	"slices"
)

func builtInMailDriver(driver MailDriver) bool {
	switch driver {
	case LogMail, PreviewMail, MemoryMail, SMTPMail, SESMail, ResendMail, CloudflareMail, PostmarkMail, MailgunMail, FailoverMail, RoundRobinMail:
		return true
	}
	return false
}

// mailCredentials returns the credential sources a mailer's transport needs:
// its own for SES, or its referenced transports' for a composition.
func (p *Plan) mailCredentials(s MailerSettings) []credentials.Name {
	var names []credentials.Name
	add := func(s MailerSettings) {
		if s.Driver == SESMail {
			name := s.API.Credentials
			if name == "" {
				name = "default"
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	add(s)
	if s.Driver == FailoverMail || s.Driver == RoundRobinMail {
		for _, name := range s.Transports {
			add(p.settings.Mail.Mailers[name])
		}
	}
	return names
}

// composedMail validates a failover/roundrobin reference list.
func (p *Plan) composedMail(s MailerSettings) error {
	if len(s.Transports) < 2 || len(s.Transports) > failover.MaxTransports {
		return fault.New(fault.Invalid, "a composed mailer requires 2 to 8 transports")
	}
	for i, name := range s.Transports {
		child, ok := p.settings.Mail.Mailers[name]
		if !ok {
			return fault.New(fault.Missing, "composed mailer transport is not configured")
		}
		if child.Driver == FailoverMail || child.Driver == RoundRobinMail || slices.Contains(s.Transports[:i], name) {
			return fault.New(fault.Invalid, "composed mailer transports must be distinct, non-composed mailers")
		}
	}
	return nil
}

func (p *Plan) mailAdapter(s MailerSettings, credential func(credentials.Name) (credentials.Provider, error)) (*ownedAdapter[email.Driver], error) {
	result := &ownedAdapter[email.Driver]{}
	http := email.HTTPConfig{Endpoint: s.API.Endpoint, Timeout: s.API.Timeout}
	switch s.Driver {
	case LogMail:
		driver, err := emaillog.New(p.options.logger)
		if err != nil {
			return nil, err
		}
		result.value = driver
	case PreviewMail:
		driver, err := emaillog.NewPreview(p.options.logger, 0)
		if err != nil {
			return nil, err
		}
		result.value = driver
	case MemoryMail:
		driver, err := memory.New(s.MemoryCapacity)
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case SMTPMail:
		driver, err := smtp.New(smtp.Config{Address: s.SMTP.Address, Security: s.SMTP.Security, Username: s.SMTP.Username, Password: s.SMTP.Password, Auth: s.SMTP.Auth, LocalName: s.SMTP.LocalName, Timeout: s.SMTP.Timeout, MaxIdle: s.SMTP.MaxIdle, IdleTimeout: s.SMTP.IdleTimeout})
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case SESMail:
		name := s.API.Credentials
		if name == "" {
			name = "default"
		}
		provider, err := credential(name)
		if err != nil {
			return nil, err
		}
		driver, err := ses.New((ses.Config{HTTP: http, Region: s.API.Region, ConfigurationSet: s.API.ConfigurationSet}).WithCredentials(provider))
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case ResendMail:
		driver, err := resend.New(resend.Config{HTTP: http, Token: s.API.Token})
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case CloudflareMail:
		driver, err := cloudflare.New(cloudflare.Config{HTTP: http, AccountID: s.API.AccountID, Token: s.API.Token})
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case PostmarkMail:
		driver, err := postmark.New(postmark.Config{HTTP: http, ServerToken: s.API.Token, MessageStream: s.API.MessageStream})
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case MailgunMail:
		driver, err := mailgun.New(mailgun.Config{HTTP: http, APIKey: s.API.Token, Domain: s.API.Domain})
		if err != nil {
			return nil, err
		}
		result.value = driver
		result.close = func(context.Context) error { driver.Close(); return nil }
	case FailoverMail, RoundRobinMail:
		if err := p.composedMail(s); err != nil {
			return nil, err
		}
		var transports []email.Driver
		var closers []func(context.Context) error
		closeAll := func(ctx context.Context) error {
			var failures []error
			for _, close := range closers {
				failures = append(failures, close(ctx))
			}
			return errors.Join(failures...)
		}
		for _, name := range s.Transports {
			child, err := p.mailAdapter(p.settings.Mail.Mailers[name], credential)
			if err != nil {
				return nil, errors.Join(err, closeAll(context.Background()))
			}
			transports = append(transports, child.value)
			if child.close != nil {
				closers = append(closers, child.close)
			}
		}
		compose := failover.New
		if s.Driver == RoundRobinMail {
			compose = failover.RoundRobin
		}
		driver, err := compose(transports...)
		if err != nil {
			return nil, errors.Join(err, closeAll(context.Background()))
		}
		result.value = driver
		result.close = closeAll
	default:
		driver, ok := p.options.mailDrivers[s.Driver]
		if !ok {
			return nil, fault.New(fault.Invalid, "unsupported mail driver")
		}
		result.value = driver
	}
	return result, nil
}
func (p *Plan) mailer(name email.MailerName, s MailerSettings) {
	owner := foundation.ProviderID(string(MailProvider(name)) + ".transport")
	key := foundation.NewKey[*ownedAdapter[email.Driver]](string(owner))
	var requires []foundation.ProviderID
	for _, name := range p.mailCredentials(s) {
		requires = append(requires, CredentialProvider(name))
	}
	p.providers = append(p.providers, adapterModule(owner, key, requires, func(r foundation.Resolver) (*ownedAdapter[email.Driver], error) {
		return p.mailAdapter(s, func(name credentials.Name) (credentials.Provider, error) {
			return foundation.Resolve(r, CredentialKey(name))
		})
	}))
	requires = []foundation.ProviderID{owner}
	if len(p.settings.Storage.Disks) > 0 {
		requires = append(requires, storageRegistryProvider)
	}
	p.providers = append(p.providers, email.Module(MailProvider(name), MailKey(name), requires, func(r foundation.Resolver) (*email.Mailer, error) {
		adapter, err := foundation.Resolve(r, key)
		if err != nil {
			return nil, err
		}
		var disks *storage.Registry
		if len(p.settings.Storage.Disks) > 0 {
			disks, err = foundation.Resolve(r, storageRegistryKey)
			if err != nil {
				return nil, err
			}
		}
		return email.New(adapter.value, disks, s.Config, nil)
	}))
}
