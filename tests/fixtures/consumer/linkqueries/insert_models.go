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

//foundry:model table=link_archives var=Archives key=ID identity=false retrieval=archiveRetrievalHooks hooks=archiveHooks
type Archive struct {
	ID        model.ID[Archive] `foundry:"default=database,member=ID,key-role=primary"`
	MemberID  model.ID[Member]  `foundry:"role=column"`
	Name      string
	Alias     value.Nullable[string]
	Tag       string          `foundry:"input=ArchiveTagInput"`
	Counter   int64           `foundry:"default=database"`
	Amount    decimal.Decimal `foundry:"role=number"`
	Payload   value.JSON[[]string]
	CreatedAt time.Time         `foundry:"role=created-at"`
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (Archive) MutateTag(input ArchiveTagInput) (string, error) {
	if input.Text == "reject" {
		return "", ErrVeto
	}
	return strings.ToLower(strings.TrimSpace(input.Text)), nil
}
