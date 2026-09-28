package invalid

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func invalid(clients *httpclient.Clients, name email.MailerName) { _, _ = clients.Client(name) }
