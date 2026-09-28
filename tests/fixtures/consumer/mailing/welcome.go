// Package mailing declares application email data and templates. Foundry owns
// rendering, storage resolution, transport, worker retries and outbox delivery.
package mailing

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

type Welcome struct {
	Recipient email.Address      `json:"recipient"`
	Name      string             `json:"name"`
	Documents []email.Attachment `json:"documents,omitempty"`
}

var WelcomeEmail = jobs.Define[Welcome]("mail.welcome", 1, jobs.DefaultPolicy("communications"))
var MailerKey = foundation.NewKey[*email.Mailer]("application.mailer")

type Sender struct {
	mailer   *email.Mailer
	from     email.Address
	template email.Template[Welcome]
}

func NewSender(mailer *email.Mailer, from email.Address) (*Sender, error) {
	if from.Validate() != nil {
		return nil, email.Construction
	}
	template, err := email.NewTemplate[Welcome](email.TemplateSource{Subject: "Welcome {{.Name}}", Text: "Hello {{.Name}}", HTML: "<p>Hello {{.Name}}</p>", MaxBytes: 16 << 10})
	if err != nil {
		return nil, err
	}
	return &Sender{mailer: mailer, from: from, template: template}, nil
}
func (s *Sender) Build(ctx context.Context, input Welcome) (email.Message, error) {
	body, err := s.template.Render(ctx, input)
	if err != nil {
		return email.Message{}, err
	}
	return body.Message(s.from, input.Recipient).Attach(input.Documents...), nil
}
func (s *Sender) Send(ctx context.Context, input Welcome) (email.Result, error) {
	message, err := s.Build(ctx, input)
	if err != nil {
		return email.Result{}, err
	}
	return s.mailer.Send(ctx, message, email.SendOptions{})
}
func (s *Sender) Declaration() (jobs.Declaration, error) {
	return WelcomeEmail.Declare(email.JobHandler(s.mailer, s.Build))
}
func EnqueueWelcome(ctx context.Context, tx *database.Tx, producer *jobs.Outbox, input Welcome) (outbox.ID[Welcome], error) {
	return WelcomeEmail.Enqueue(ctx, tx, producer, input, jobs.Options[Welcome]{})
}
func Module(driver email.Driver, requires ...foundation.ProviderID) foundation.Module {
	return email.Module("application.email", MailerKey, requires, func(foundation.Resolver) (*email.Mailer, error) {
		return email.New(driver, nil, email.DefaultConfig(), nil)
	})
}
