package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
)

type Other struct{}

var _ = orm.Set(recovering.MemberRecords.EmailRevision, challenge.Revision[Other]{})
