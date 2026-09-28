package mailing_test

import (
	"context"
	"strings"
	"testing"

	"foundry.test/consumer/mailing"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/testkit"
	emailtest "github.com/weiloon1234/Foundry-Go/testkit/email"
)

func TestConsumerEmailModuleTypedRenderingAndQueueCapture(t *testing.T) {
	driver, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	app := testkit.Start(t, foundry.New().Register(mailing.Module(driver)))
	mailer, err := foundation.Resolve(app.Services(), mailing.MailerKey)
	if err != nil {
		t.Fatal(err)
	}
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := email.ParseAddress("recipient@example.test")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := mailing.NewSender(mailer, from)
	if err != nil {
		t.Fatal(err)
	}
	input := mailing.Welcome{Recipient: recipient, Name: "<script>"}
	result, err := sender.Send(t.Context(), input)
	if err != nil || !result.Accepted {
		t.Fatal(err)
	}
	emailtest.AssertCount(t, driver, 1)
	emailtest.AssertRecipientCount(t, driver, recipient, 1)
	if driver.Messages()[0].Message().HTMLBody() != "<p>Hello &lt;script&gt;</p>" {
		t.Fatal("consumer template did not escape")
	}
	if _, err := sender.Declaration(); err != nil {
		t.Fatal(err)
	}
	pending, err := mailing.WelcomeEmail.Capture(t.Context(), input, jobs.Options[mailing.Welcome]{})
	if err != nil {
		t.Fatal(err)
	}
	input.Name = "changed"
	if strings.Contains(pending.Envelope().PayloadJSON(), "changed") {
		t.Fatal("queue snapshot changed")
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mailer.Done():
	default:
		t.Fatal("module did not drain mailer")
	}
	if _, err := sender.Send(t.Context(), input); err == nil {
		t.Fatal("shutdown mailer accepted work")
	}
}
