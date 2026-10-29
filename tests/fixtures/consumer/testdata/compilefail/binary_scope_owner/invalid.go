package compilefail

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/binarymodels"
	"foundry.test/consumer/linkqueries"
)

var _ = linkqueries.Members.Query().Where(orm.InputColumnEq(binarymodels.Records.Body, binarymodels.Payload{1}))
