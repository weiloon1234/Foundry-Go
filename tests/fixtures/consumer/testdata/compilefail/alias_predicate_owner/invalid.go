package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type leftTag struct{}
type rightTag struct{}
type outsideTag struct{}

var a = query.As[leftTag](models.QueryUsers(), "a")
var b = query.As[rightTag](models.QueryUsers(), "b")
var af, bf = models.UserFieldsAt(a.Scope()), models.UserFieldsAt(b.Scope())
var j = query.LeftJoin(a, b, query.On(af.IntroducerID, bf.ID))
var invalid = reports.ProjectReferralRow(a).Query().Where(bf.Email.Eq("x"))
