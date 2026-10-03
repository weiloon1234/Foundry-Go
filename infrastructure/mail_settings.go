package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/smtp"
	"github.com/weiloon1234/Foundry-Go/secret"
	"time"
)

type MailDriver string

const (
	LogMail        MailDriver = "log"
	MemoryMail     MailDriver = "memory"
	SMTPMail       MailDriver = "smtp"
	SESMail        MailDriver = "ses"
	ResendMail     MailDriver = "resend"
	CloudflareMail MailDriver = "cloudflare"
	PostmarkMail   MailDriver = "postmark"
	MailgunMail    MailDriver = "mailgun"
	// FailoverMail tries Transports in order; RoundRobinMail starts each send at
	// the next one. Both move on only after a known non-acceptance (Transient).
	FailoverMail   MailDriver = "failover"
	RoundRobinMail MailDriver = "roundrobin"
	// PreviewMail logs the rendered message for local development; it
	// records private content and delivers nothing.
	PreviewMail MailDriver = "preview"
)

// SMTPSettings configures the SMTP transport. Auth selects plain, login or
// xoauth2 (the password is then the OAuth access token). LocalName overrides
// the EHLO name. MaxIdle > 0 reuses up to that many idle connections for
// IdleTimeout.
type SMTPSettings struct {
	Address     string
	Security    smtp.Security
	Username    string
	Password    secret.String
	Auth        smtp.Mechanism
	LocalName   string
	Timeout     time.Duration
	MaxIdle     int
	IdleTimeout time.Duration
}
type MailAPISettings struct {
	Endpoint         string
	Timeout          time.Duration
	Token            secret.String
	AccountID        string
	Domain           string
	MessageStream    string
	Region           string
	ConfigurationSet string
	Credentials      credentials.Name
}

// MailerSettings keeps transport configuration separate from typed message data.
// From is required when enabling a mailer; defaults simulate acceptance through logs.
//
// Transports names 2 to 8 other configured mailers composed by the failover and
// roundrobin drivers. Each composition owns its own transport instances built
// from those mailers' settings; referenced mailers cannot themselves compose.
//
//foundry:config
type MailerSettings struct {
	Driver         MailDriver
	Config         email.Config
	SMTP           SMTPSettings
	API            MailAPISettings
	MemoryCapacity int
	Transports     []email.MailerName `config:",json"`
}

func DefaultMailerSettings() MailerSettings {
	return MailerSettings{Driver: LogMail, Config: email.DefaultConfig(), SMTP: SMTPSettings{Security: smtp.STARTTLS, Timeout: email.DefaultConfig().Timeout, MaxIdle: 2, IdleTimeout: 30 * time.Second}, API: MailAPISettings{Timeout: email.DefaultHTTPConfig().Timeout}, MemoryCapacity: 128}
}

type Mailers map[email.MailerName]MailerSettings

func (m *Mailers) UnmarshalText(data []byte) error {
	schema, err := MailerSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(email.MailerName) MailerSettings { return DefaultMailerSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type MailSettings struct {
	Default email.MailerName
	Mailers Mailers `config:",secret"`
}

func DefaultMailSettings() MailSettings { return MailSettings{Default: "default"} }
