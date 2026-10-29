package outer_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type referralAlias struct{}
type sponsorAlias struct{}
type purchaseAlias struct{}

func TestPostgresOuterAndChainedJoins(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := rows.As[referralAlias](models.Users.Query(), "referral")
		sponsors := rows.As[sponsorAlias](models.Users.Query(), "sponsor")
		r := rows.Scope(referrals)
		s := rows.Scope(sponsors)
		joined := rows.LeftJoin(referrals, sponsors, rows.On(expr.At(r, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(s, orm.KeyExpr(models.Users.ID))))
		referralScope := rows.LeftScope(joined, rows.Scope(referrals))
		sponsorScope := rows.NullableRightScope(joined, rows.Scope(sponsors))
		sponsor := sponsorScope
		purchases := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		p := rows.Scope(purchases)
		// A third source preserves earlier nullable scopes through another left join.
		chain := rows.LeftJoin(joined, purchases, rows.On(expr.AtNullable(sponsor, orm.KeyExpr(models.Users.ID)), expr.At(p, orm.ColumnExpr(models.Orders.BuyerID))))
		cr := rows.LeftScope(chain, referralScope)
		cs := rows.LeftNullableScope(chain, sponsorScope)
		cp := rows.NullableRightScope(chain, rows.Scope(purchases))
		chainReport := rows.Select(chain, reports.ReferralOrderRows, rows.MapSelected(reports.ReferralOrderRows.UserID, expr.Row(expr.At(cr, orm.KeyExpr(models.Users.ID))).Value()), rows.MapSelected(reports.ReferralOrderRows.IntroducerID, expr.Row(expr.AtNullable(cs, orm.KeyExpr(models.Users.ID))).Value()), rows.MapSelected(reports.ReferralOrderRows.IntroducerOrderID, expr.Row(expr.AtNullable(cp, orm.KeyExpr(models.Orders.ID))).Value()))
		chainRows, err := rows.Read(chainReport).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(chainRows) != 6 {
			t.Error("chained joins changed one-to-many row multiplicity")
		}
		if n, err := rows.Read(chainReport.Where(expr.AtNullable(cp, orm.KeyExpr(models.Orders.ID)).IsNull())).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("chained outer nullability lost", err)
		}
		// Right and full joins require nullable left field sets.
		right := rows.RightJoin(referrals, sponsors, rows.On(expr.At(r, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(s, orm.KeyExpr(models.Users.ID))))
		rl := rows.NullableLeftScope(right, rows.Scope(referrals))
		rr := rows.RightScope(right, rows.Scope(sponsors))
		rightReport := rows.Select(right, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Row(expr.AtNullable(rl, orm.KeyExpr(models.Users.ID))).Value()), rows.MapSelected(reports.UserPairs.RightID, expr.Nullable(expr.Row(expr.At(rr, orm.KeyExpr(models.Users.ID))).Value())))
		if n, err := rows.Read(rightReport).Count(t.Context(), counter); err != nil || n != 5 {
			t.Error("right join did not preserve unmatched sponsors", err)
		}
		if n, err := rows.Read(rightReport.Where(expr.AtNullable(rl, orm.KeyExpr(models.Users.ID)).IsNull())).Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("right join null extension incorrect", err)
		}
		full := rows.FullJoin(referrals, sponsors, rows.On(expr.At(r, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(s, orm.KeyExpr(models.Users.ID))))
		fl := rows.NullableLeftScope(full, rows.Scope(referrals))
		fright := rows.NullableRightScope(full, rows.Scope(sponsors))
		fullReport := rows.Select(full, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Row(expr.AtNullable(fl, orm.KeyExpr(models.Users.ID))).Value()), rows.MapSelected(reports.UserPairs.RightID, expr.Row(expr.AtNullable(fright, orm.KeyExpr(models.Users.ID))).Value()))
		if n, err := rows.Read(fullReport).Count(t.Context(), counter); err != nil || n != 6 {
			t.Error("full join changed unmatched sides", err)
		}
		return nil
	})
}
