package invalid

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid(mailers *email.Mailers, name jobs.ConnectionName) { _, _ = mailers.Mailer(name) }
