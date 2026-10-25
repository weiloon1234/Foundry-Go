package linkqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Keep the legacy creation-adapter oracle until the lead retires that public bridge.
// ORM lookup callers use sealed assignments and have no application creation adapter.
func TestPostgresLegacyCreationAdapterIsLazy(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := t.Context()
		q := linkqueries.QueryLinkMemberships()
		fields := linkqueries.MembershipFields()
		selected := q.Where(fields.Role.Eq("admin"))
		created, err := selected.FirstOrCreate(ctx, tx, linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("admin"))
		if err != nil {
			return err
		}
		probe := &unusedMembershipCreation{}
		observed, err := selected.Query.FirstOrInsert(ctx, tx, probe)
		if err != nil || observed.ID != created.ID || probe.calls != 0 {
			return errors.New("found lookup prepared its unused creation bridge")
		}
		if _, err := q.Where(fields.Role.Eq("absent")).Query.FirstOrInsert(ctx, tx, probe); !errors.Is(err, linkqueries.ErrVeto) || probe.calls != 1 {
			return errors.New("missing lookup did not prepare exactly one creation bridge")
		}
		return nil
	})
}

// The public runtime draft bridge makes lazy branch preparation observable
// without replacing UUID entropy or invoking a real mutation on the found path.
type unusedMembershipCreation struct{ calls int }

func (d *unusedMembershipCreation) FoundryCreateMutation(query.Mutation[linkqueries.Membership]) (query.Mutation[linkqueries.Membership], error) {
	d.calls++
	return query.Mutation[linkqueries.Membership]{}, linkqueries.ErrVeto
}
