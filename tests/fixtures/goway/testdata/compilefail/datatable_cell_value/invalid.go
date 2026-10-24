package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Member](validation.DefineField("name", func(r reporting.Member) string { return r.Name }), "reports.name").ExportAs(datatable.ScalarCell(foundryhttp.IntegerQuery[int64]()))
