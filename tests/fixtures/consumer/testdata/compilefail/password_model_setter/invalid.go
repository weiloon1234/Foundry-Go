package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/passwords"

func bad() { _ = orm.ColumnSet(passwords.AccountRecords.Digest, "plaintext") }
