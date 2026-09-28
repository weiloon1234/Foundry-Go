package invalid

import (
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
)

func bad(login *passwords.Login, th lockout.Throttle[int]) { login.WithLockout(th) }
