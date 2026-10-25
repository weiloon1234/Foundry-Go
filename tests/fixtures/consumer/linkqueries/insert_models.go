package linkqueries

import (
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ArchiveTagInput struct{ Text string }

type Archive struct {
	ID        model.ID[Archive] `foundry:"default=database"`
	MemberID  model.ID[Member]
	Name      string
	Alias     value.Nullable[string]
	Tag       string
	Counter   int64 `foundry:"default=database"`
	Amount    decimal.Decimal
	Payload   value.JSON[[]string]
	CreatedAt time.Time
	UpdatedAt temporal.DateTime
}

func (Archive) MutateTag(input ArchiveTagInput) (string, error) {
	if input.Text == "reject" {
		return "", ErrVeto
	}
	return strings.ToLower(strings.TrimSpace(input.Text)), nil
}
