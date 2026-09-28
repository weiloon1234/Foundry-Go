package invalid

import (
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
)

type Email string

func bad(notice lockout.Notice[Email]) { var key int = notice.Key(); _ = key }
