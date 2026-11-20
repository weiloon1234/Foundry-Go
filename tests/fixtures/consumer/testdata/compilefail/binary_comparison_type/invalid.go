package compilefail

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/binarymodels"

var _ = orm.Eq(binarymodels.Records.Body, "text")
