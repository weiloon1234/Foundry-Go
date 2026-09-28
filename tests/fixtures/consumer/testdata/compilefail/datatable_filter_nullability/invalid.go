package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.DefineColumn[reporting.Member](reporting.MemberRowValidationFields().Nickname, reporting.NicknameLabel).FilterBy(datatable.Where(foundryhttp.StringQuery[string](), reporting.MemberFields().Name))
