package invalid

import "github.com/weiloon1234/Foundry-Go/email"

func invalid(message email.Message) {
	_, _ = message.AttachStored("/tmp/document.txt")
}
