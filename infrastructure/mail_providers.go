package infrastructure

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/email"
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
)

func builtInMailDriver(driver MailDriver) bool {
	switch driver {
	case LogMail, MemoryMail, SMTPMail, SESMail, ResendMail, PostmarkMail, MailgunMail:
		return true
	}
	return false
}
func (p *Plan) mailAdapter(s MailerSettings, provider credentials.Provider) (*ownedAdapter[email.Driver], error) {
	result := &ownedAdapter[email.Driver]{}
	http := email.HTTPConfig{Endpoint: s.API.Endpoint, Timeout: s.API.Timeout}
	switch s.Driver {
	case LogMail:
		driver, err := emaillog.New(p.options.logger)
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
		driver, err := smtp.New(smtp.Config{Address: s.SMTP.Address, Security: s.SMTP.Security, Username: s.SMTP.Username, Password: s.SMTP.Password, Timeout: s.SMTP.Timeout})
		if err != nil {
			return nil, err
		}
		result.value = driver
	case SESMail:
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
	if s.Driver == SESMail {
		requires = append(requires, CredentialProvider(s.API.Credentials))
	}
	p.providers = append(p.providers, adapterModule(owner, key, requires, func(r foundation.Resolver) (*ownedAdapter[email.Driver], error) {
		var provider credentials.Provider
		if s.Driver == SESMail {
			var err error
			provider, err = foundation.Resolve(r, CredentialKey(s.API.Credentials))
			if err != nil {
				return nil, err
			}
		}
		return p.mailAdapter(s, provider)
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
