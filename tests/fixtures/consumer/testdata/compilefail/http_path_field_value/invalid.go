package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type UserPath struct{ ID int }

var invalid = foundryhttp.Param[UserPath, string]("user", foundryhttp.StringPath[string](), func(path *UserPath) *int { return &path.ID })
