package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type referralAlias struct{}
type sponsorAlias struct{}
type ReferralScope = rows.Left[rows.Alias[referralAlias, models.User], rows.Alias[sponsorAlias, models.User]]

// doc-example:begin outer-joined-text
func ReferralTextFilters() (datatable.FilterSource[ReferralScope, value.Nullable[string]], datatable.FilterSource[ReferralScope, value.Nullable[string]]) {
	referrals := rows.As[referralAlias](models.Users.Query(), "referral")
	sponsors := rows.As[sponsorAlias](models.Users.Query(), "sponsor")
	joined := rows.LeftJoin(referrals, sponsors, rows.On(
		expr.At(rows.Scope(referrals), orm.NullableColumnExpr(models.Users.IntroducerID)),
		expr.At(rows.Scope(sponsors), orm.KeyExpr(models.Users.ID)),
	))
	sponsor := rows.NullableRightScope(joined, rows.Scope(sponsors))
	text := http.StringQuery[string]()
	email := expr.TextNullableRowValue(expr.AtNullable(sponsor, orm.TextExpr(models.Users.Email)))
	nickname := expr.TextNullableRowValue(expr.AtNullableValue(sponsor, orm.NullableTextExpr(models.Users.Nickname)))
	return datatable.NullableWhere(text, expr.NullableTextFilter(email)),
		datatable.NullableWhere(text, expr.NullableTextFilter(nickname))
}

// doc-example:end outer-joined-text

// doc-example:begin nullable-ordered-filter
func MeasurementScoreFilter() datatable.FilterSource[models.Measurement, value.Nullable[float64]] {
	score := expr.OrderNullableRow(expr.Value(orm.NullableNumberExpr(models.Measurements.Score)))
	return datatable.NullableWhere(http.FloatQuery[float64](), expr.NullableOrderFilter(score))
}

// doc-example:end nullable-ordered-filter
