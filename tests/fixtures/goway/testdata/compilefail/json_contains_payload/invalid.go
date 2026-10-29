package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() { _ = orm.JSONContains(jsonpaths.Documents.Settings, value.JSON[[]string]{}) }
