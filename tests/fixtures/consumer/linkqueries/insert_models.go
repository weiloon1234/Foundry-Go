package linkqueries

import (
	"context"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ArchiveTagInput struct{ Text string }

//foundry:model table=link_archives hooks=archiveHooks retrieval=archiveReads
type Archive struct {
	ID       model.ID[Archive] `foundry:"default=database"`
	MemberID model.ID[Member]
	Name     string
	Alias    value.Nullable[string]
	// Foundry field behavior (generated): Archive.Tag retains stored link_archives.tag. Custom setter: [Archive.MutateTag] transforms assigned values during persistence through [ArchiveDraft.SetTag]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts linkqueries.ArchiveTagInput and produces stored string; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	Tag     string
	Counter int64 `foundry:"default=database"`
	Amount  decimal.Decimal
	Payload value.JSON[[]string]
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Archive) MutateTag(input ArchiveTagInput) (string, error) {
	if input.Text == "reject" {
		return "", ErrVeto
	}
	return strings.ToLower(strings.TrimSpace(input.Text)), nil
}

func archiveHooks() ArchiveHooks {
	return ArchiveHooks{Creating: func(ctx context.Context, _ *database.Tx, _ *ArchiveDraft) error {
		if trace := traceFrom(ctx); trace != nil {
			trace.Steps = append(trace.Steps, "archive.creating")
			return ErrVeto
		}
		return nil
	}}
}
func archiveReads() ArchiveRetrievalHooks {
	return ArchiveRetrievalHooks{Retrieved: func(ctx context.Context, _ database.Executor, _ Archive) error { recordRead(ctx); return nil }}
}
