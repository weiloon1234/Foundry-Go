package invalid

import (
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad() {
	_ = auth.PasswordModel[passwords.Account, string]{Hash: func(passwords.Account) string { return "plaintext" }}
}
