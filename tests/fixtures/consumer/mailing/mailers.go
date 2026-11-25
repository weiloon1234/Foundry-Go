package mailing

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/email"
)

// doc-example:begin configured-mailer
func ResolveMailer(app *application.App, name email.MailerName) (*email.Mailer, error) {
	return app.Resources().Mailers.Mailer(name)
}

// doc-example:end configured-mailer
