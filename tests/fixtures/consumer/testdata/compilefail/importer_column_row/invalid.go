package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable/importer"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = importer.Spec[reporting.MemberImport]{Columns: []importer.Column[reporting.MemberImport]{
	importer.Field("Label", foundryhttp.StringQuery[string](), func(row *reporting.OrderRow, v string) { row.Label = v }),
}}
