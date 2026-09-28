package invalid

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
)

type Other struct{}

var _ = recovering.MemberDraft{}.SetEmailRevision(challenge.Revision[Other]{})
