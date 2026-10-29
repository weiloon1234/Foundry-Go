package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/inputqueries"
)

var _ = orm.InputTextEq(inputqueries.Members.Email, inputqueries.EmailInput{})
