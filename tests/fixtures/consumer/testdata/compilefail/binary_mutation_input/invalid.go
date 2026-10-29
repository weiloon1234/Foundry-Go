package compilefail

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/binarymodels"

var _ = orm.InputColumnSet(binarymodels.Records.Body, binarymodels.Payload{1})
