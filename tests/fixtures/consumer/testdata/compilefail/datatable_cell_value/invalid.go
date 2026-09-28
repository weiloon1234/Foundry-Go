package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.DefineColumn[reporting.Member](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel).ExportAs(datatable.ScalarCell(foundryhttp.IntegerQuery[int64]()))
