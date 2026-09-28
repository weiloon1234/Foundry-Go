package compilefail

import "github.com/weiloon1234/Foundry-Go/email"

func invalid(from string, recipient email.Address) { _ = email.NewMessage(from, "Subject", recipient) }
