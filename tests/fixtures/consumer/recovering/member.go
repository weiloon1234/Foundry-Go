package recovering

import (
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Member is an independent recovery consumer fixture, not a starter app.
//
//foundry:model table=recovery_members var=MemberRecords key=ID identity=false hooks=memberHooks
type Member struct {
	ID    model.ID[Member]
	Email string
	// EmailRevision is managed by ormMemberHooks. It changes with Email and is never
	// restored with an old address; DTOs should not expose this persistence value.
	EmailRevision challenge.Revision[Member] `foundry:"role=column"`
	Password      password.Hash
	EmailVerified bool `foundry:"role=column"`
	Enabled       bool `foundry:"role=column"`
}
