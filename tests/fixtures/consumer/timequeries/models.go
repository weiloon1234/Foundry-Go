// Package timequeries exercises framework-managed model time in a consumer.
package timequeries

import (
	"errors"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

//foundry:model table=time_members hooks=memberHooks
type Member struct {
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Name retains stored time_members.name. Custom setter: [Member.MutateName] transforms assigned values during persistence through [MemberDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name string
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time `foundry:"column=created_on"`
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime `foundry:"column=changed_on"`
}

//foundry:model table=time_manual timestamps=false
type Manual struct {
	ID        model.ID[Manual]
	CreatedAt time.Time
	UpdatedAt temporal.DateTime
}

var Veto = errors.New("timestamp fixture veto")

func (Member) MutateName(input string) (string, error) {
	if input == "reject" {
		return "", Veto
	}
	return strings.TrimSpace(input), nil
}
