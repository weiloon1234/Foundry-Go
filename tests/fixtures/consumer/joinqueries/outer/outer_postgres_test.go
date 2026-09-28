package outer_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"testing"
)

type referralAlias struct{}
type sponsorAlias struct{}
type purchaseAlias struct{}

func TestPostgresOuterAndChainedJoins(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := query.As[referralAlias](models.QueryUsers(), "referral")
		sponsors := query.As[sponsorAlias](models.QueryUsers(), "sponsor")
		r := models.UserFieldsAt(referrals.Scope())
		s := models.UserFieldsAt(sponsors.Scope())
		joined := query.LeftJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID))
		referralScope := query.LeftScope(joined, referrals.Scope())
		sponsorScope := query.NullableRightScope(joined, sponsors.Scope())
		sponsor := models.UserNullableFieldsAt(sponsorScope)
		purchases := query.As[purchaseAlias](models.QueryOrders(), "purchase")
		p := models.OrderFieldsAt(purchases.Scope())
		// A third source preserves earlier nullable scopes through another left join.
		chain := query.LeftJoin(joined, purchases, query.On(sponsor.ID, p.BuyerID))
		cr := models.UserFieldsAt(query.LeftScope(chain, referralScope))
		cs := models.UserNullableFieldsAt(query.LeftNullableScope(chain, sponsorScope))
		cp := models.OrderNullableFieldsAt(query.NullableRightScope(chain, purchases.Scope()))
		chainReport := reports.ProjectReferralOrderRow(chain).SelectUserID(cr.ID.Value()).SelectIntroducerID(cs.ID.Value()).SelectIntroducerOrderID(cp.ID.Value()).Query()
		chainRows, err := chainReport.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(chainRows) != 6 {
			t.Error("chained joins changed one-to-many row multiplicity")
		}
		if n, err := chainReport.Where(cp.ID.IsNull()).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("chained outer nullability lost", err)
		}
		// Right and full joins require nullable left field sets.
		right := query.RightJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID))
		rl := models.UserNullableFieldsAt(query.NullableLeftScope(right, referrals.Scope()))
		rr := models.UserFieldsAt(query.RightScope(right, sponsors.Scope()))
		rightReport := reports.ProjectUserPair(right).SelectLeftID(rl.ID.Value()).SelectRightID(query.Nullable(rr.ID.Value())).Query()
		if n, err := rightReport.Count(t.Context(), counter); err != nil || n != 5 {
			t.Error("right join did not preserve unmatched sponsors", err)
		}
		if n, err := rightReport.Where(rl.ID.IsNull()).Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("right join null extension incorrect", err)
		}
		full := query.FullJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID))
		fl := models.UserNullableFieldsAt(query.NullableLeftScope(full, referrals.Scope()))
		fright := models.UserNullableFieldsAt(query.NullableRightScope(full, sponsors.Scope()))
		fullReport := reports.ProjectUserPair(full).SelectLeftID(fl.ID.Value()).SelectRightID(fright.ID.Value()).Query()
		if n, err := fullReport.Count(t.Context(), counter); err != nil || n != 6 {
			t.Error("full join changed unmatched sides", err)
		}
		return nil
	})
}
