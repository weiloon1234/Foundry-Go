package invalid

import (
	"context"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var cookie = foundryhttp.DefineCookie("flag", foundryhttp.BoolCookie[bool](), foundryhttp.DefaultCookieOptions())
var incompatible int
var _ = cookie.Set(context.Background(), nil, incompatible)
