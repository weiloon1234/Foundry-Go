package email_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	emailtest "github.com/weiloon1234/Foundry-Go/testkit/email"
)

func TestMemoryHelperUsesNormalMailerAndOwnsPrivateDataCleanup(t *testing.T) {
	var retained *memory.Driver
	t.Run("owner", func(t *testing.T) {
		retained = emailtest.New(t, 2)
		other := emailtest.New(t, 2)
		mailer, err := email.New(retained, nil, email.DefaultConfig(), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := mailer.Close(ctx); err != nil {
				t.Error(err)
			}
		})
		from, err := email.ParseAddress("sender@example.test")
		if err != nil {
			t.Fatal(err)
		}
		to, err := email.ParseAddress("recipient@example.test")
		if err != nil {
			t.Fatal(err)
		}
		message := email.NewMessage(from, "Private subject", to).Text("Private body")
		if _, err := mailer.Send(t.Context(), message, email.SendOptions{}); err != nil {
			t.Fatal(err)
		}
		emailtest.AssertCount(t, retained, 1)
		emailtest.AssertRecipientCount(t, retained, to, 1)
		emailtest.AssertCount(t, other, 0)
	})
	if len(retained.Messages()) != 0 {
		t.Fatal("cleanup retained private email data")
	}
}
