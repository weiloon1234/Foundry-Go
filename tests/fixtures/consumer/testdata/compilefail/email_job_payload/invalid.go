package compilefail

import (
	"context"
	"foundry.test/consumer/mailing"
	"github.com/weiloon1234/Foundry-Go/email"
)

type Other struct{ Name string }

func invalid(m *email.Mailer) {
	_, _ = mailing.WelcomeEmail.Declare(email.JobHandler(m, func(context.Context, Other) (email.Message, error) { return email.Message{}, nil }))
}
