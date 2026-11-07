package background

import (
	"context"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// doc-example:begin localized-mail-job
type LocalizedWelcome struct {
	Recipient email.Address      `json:"recipient"`
	Name      string             `json:"name"`
	Locale    i18n.LocaleID      `json:"locale"`
	Documents []email.Attachment `json:"documents,omitempty"`
}

var LocalizedWelcomeJob = jobs.Define[LocalizedWelcome]("mail.localized-welcome", 1, jobs.DefaultPolicy("communications"))

func LocalizedWelcomeDeclaration(mailerName email.MailerName) application.JobDeclaration {
	return application.Job(LocalizedWelcomeJob, func(services application.Services) (jobs.Handler[LocalizedWelcome], error) {
		mailer, err := services.Mailers.Mailer(mailerName)
		if err != nil {
			return nil, err
		}
		catalog, err := services.Locales()
		if err != nil {
			return nil, err
		}
		return email.JobHandler(mailer, func(ctx context.Context, input LocalizedWelcome) (email.Message, error) {
			ctx, err := i18n.WithLocale(ctx, catalog, input.Locale)
			if err != nil {
				return email.Message{}, err
			}
			welcome, err := localization.WelcomeArgsMessage().Format(ctx, catalog, input.Locale, localization.WelcomeArgs{Name: input.Name})
			if err != nil {
				return email.Message{}, err
			}
			return mailer.Message(welcome.Text, input.Recipient).Text(welcome.Text).Locale(welcome.Locale).Attach(input.Documents...), nil
		}), nil
	})
}

// doc-example:end localized-mail-job
