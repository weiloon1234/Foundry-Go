package recovering

import (
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Member is an independent recovery consumer fixture, not a starter app.
//
//foundry:model table=recovery_members hooks=memberHooks
type Member struct {
	ID    model.ID[Member]
	Email string
	// EmailRevision is managed by memberHooks. It changes with Email and is never
	// restored with an old address; DTOs should not expose this persistence value.
	EmailRevision challenge.Revision[Member]
	// Foundry field behavior (generated): Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Password      password.Hash
	EmailVerified bool
	Enabled       bool
}
