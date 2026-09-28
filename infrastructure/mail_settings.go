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
	LogMail      MailDriver = "log"
	MemoryMail   MailDriver = "memory"
	SMTPMail     MailDriver = "smtp"
	SESMail      MailDriver = "ses"
	ResendMail   MailDriver = "resend"
	PostmarkMail MailDriver = "postmark"
	MailgunMail  MailDriver = "mailgun"
)

type SMTPSettings struct {
	Address  string
	Security smtp.Security
	Username string
	Password secret.String
	Timeout  time.Duration
}
type MailAPISettings struct {
	Endpoint         string
	Timeout          time.Duration
	Token            secret.String
	Domain           string
	MessageStream    string
	Region           string
	ConfigurationSet string
	Credentials      credentials.Name
}

// MailerSettings keeps transport configuration separate from typed message data.
// From is required when enabling a mailer; defaults simulate acceptance through logs.
//
//foundry:config
type MailerSettings struct {
	Driver         MailDriver
	Config         email.Config
	SMTP           SMTPSettings
	API            MailAPISettings
	MemoryCapacity int
}

func DefaultMailerSettings() MailerSettings {
	return MailerSettings{Driver: LogMail, Config: email.DefaultConfig(), SMTP: SMTPSettings{Security: smtp.STARTTLS, Timeout: email.DefaultConfig().Timeout}, API: MailAPISettings{Timeout: email.DefaultHTTPConfig().Timeout}, MemoryCapacity: 128}
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
