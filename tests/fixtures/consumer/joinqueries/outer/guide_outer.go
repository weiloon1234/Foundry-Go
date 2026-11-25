// Package outer exercises typed outer joins.
package outer

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin join-aliases
type ReferralAlias struct{}
type SponsorAlias struct{}

// doc-example:end join-aliases

type referralSponsor = rows.Left[rows.Alias[ReferralAlias, models.User], rows.Alias[SponsorAlias, models.User]]

// ReferralSponsors builds the guide's left join of users to their sponsors.
func ReferralSponsors() (rows.Aliased[ReferralAlias, models.User], rows.Aliased[SponsorAlias, models.User], rows.LeftJoined[rows.Alias[ReferralAlias, models.User], rows.Alias[SponsorAlias, models.User]], expr.Scope[referralSponsor, models.User], expr.NullableScope[referralSponsor, models.User]) {
	// doc-example:begin model-joins
	referrals := rows.As[ReferralAlias](models.Users.Query(), "referral")
	sponsors := rows.As[SponsorAlias](models.Users.Query(), "sponsor")
	r := rows.Scope(referrals)
	s := rows.Scope(sponsors)
	joined := rows.LeftJoin(referrals, sponsors, rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)))
	referralScope := rows.LeftScope(joined, rows.Scope(referrals))
	sponsorScope := rows.NullableRightScope(joined, rows.Scope(sponsors))
	// doc-example:end model-joins
	return referrals, sponsors, joined, referralScope, sponsorScope
}
