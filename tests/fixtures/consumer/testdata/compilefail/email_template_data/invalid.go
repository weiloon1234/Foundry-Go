package compilefail

import (
	"context"
	"foundry.test/consumer/mailing"
	"github.com/weiloon1234/Foundry-Go/email"
)

type Other struct{ Name string }

func invalid(t email.Template[mailing.Welcome]) { _, _ = t.Render(context.Background(), Other{}) }
