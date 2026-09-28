package reporting_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func TestPublicDatatablesUseGeneratedRowsPoliciesAndRelationScopes(t *testing.T) {
	f := openFixture(t)
	scope := f.scope(t)
	authority := reporting.FromGuard(f.guard)
	request := datatable.Request{Page: 1, Size: 1, Search: "aDA", Filters: []datatable.Filter{{Op: datatable.Equal, Column: "state", Values: []string{"active"}}}}
	page, err := reporting.ListMembers(scope.Context(), f.manager, f.guard, request)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != f.members[0] || page.Items[0].Name != "Ada" {
		t.Fatal("generated computed projection or enum filtering failed", page, err)
	}
	request.Page = 2
	next, err := reporting.Members.Query(scope.Context(), f.manager, authority, request)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != f.members[1] {
		t.Fatal("generated unique ID did not stabilize equal names", err)
	}
	for _, input := range []struct {
		label string
		count int64
	}{{"priority", 2}, {"trashed", 0}, {"hidden", 0}, {"foreign", 0}} {
		count, err := reporting.Members.Count(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Equal, Column: "orderLabel", Values: []string{input.label}}}})
		if err != nil || count != input.count {
			t.Fatal("relation filter crossed visibility scope", input.label, count, err)
		}
	}
	nullable, err := reporting.Members.Query(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.IsNull, Column: "nickname"}}})
	if err != nil || nullable.Total != 1 || !nullable.Items[0].Nickname.IsNull() {
		t.Fatal("generated nullable field lost null", err)
	}
	exact, err := reporting.Members.Query(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Greater, Column: "balance", Values: []string{"9007199254740993.124"}}}})
	if err != nil || exact.Total != 1 || exact.Items[0].Balance.String() != "9007199254740993.125" {
		t.Fatal("exact decimal was rounded", err)
	}
	if _, err := reporting.Members.Count(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Equal, Column: "state", Values: []string{"other"}}}}); err == nil {
		t.Fatal("undeclared enum value accepted")
	}
	info, err := reporting.Members.Inspect(scope.Context(), f.manager, authority)
	if err != nil || len(info.Columns) != 5 || len(info.Filters) != 1 {
		t.Fatal("manifest description lost declared capabilities", err)
	}
	rowSchema, err := reporting.MemberRowJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	// Description is a serialization boundary; omitted empty metadata slices
	// and nil slices have the same contract representation.
	expected, err := json.Marshal(rowSchema)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(info.Row)
	if err != nil || !bytes.Equal(expected, actual) {
		t.Fatal("table invented another DTO schema", err)
	}
	if scalar, err := reporting.MemberRowJSON().DescribeScalarProperty("balance"); err != nil || scalar.Value.Format != contract.DecimalFormat {
		t.Fatal("generated decimal metadata changed", err)
	}
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_operators SET can_view=false`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fresh := f.scope(t)
	if _, err := reporting.Members.Count(fresh.Context(), f.manager, authority, datatable.Request{}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("table trusted an earlier request's permission", err)
	}
}

func TestPublicJoinedAndGroupedReportsKeepCountsAndFilterPhases(t *testing.T) {
	f := openFixture(t)
	scope := f.scope(t)
	authority := reporting.FromGuard(f.guard)
	orders, err := reporting.Orders.Query(scope.Context(), f.manager, authority, datatable.Request{Search: "ada"})
	if err != nil || orders.Total != 3 || len(orders.Items) != 3 {
		t.Fatal("joined source lost tenant or soft-delete constraints", orders, err)
	}
	filtered, err := reporting.Orders.Count(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Equal, Column: "label", Values: []string{"priority"}}}})
	if err != nil || filtered != 2 {
		t.Fatal("joined filter count changed", filtered, err)
	}
	totals, err := reporting.Totals.Query(scope.Context(), f.manager, authority, datatable.Request{Size: 1})
	if err != nil || totals.Total != 2 || len(totals.Items) != 1 || totals.Items[0].MemberID != f.members[0] || totals.Items[0].Count != 2 {
		t.Fatal("grouped pagination counted source rows instead of groups", totals, err)
	}
	total, present := totals.Items[0].Total.Get()
	if !present || total != decimal.FromInt64(30) {
		t.Fatal("grouped amount included deleted or foreign source rows")
	}
	request := datatable.Request{Filters: []datatable.Filter{{Op: datatable.Greater, Column: "count", Values: []string{"1"}}}}
	count, err := reporting.Totals.Count(scope.Context(), f.manager, authority, request)
	if err != nil || count != 1 {
		t.Fatal("HAVING was not applied to grouped count", count, err)
	}
	request.Filters = append(request.Filters, datatable.Filter{Op: datatable.Equal, Column: "memberId", Values: []string{f.members[0].String()}})
	if count, err := reporting.Totals.Count(scope.Context(), f.manager, authority, request); err != nil || count != 1 {
		t.Fatal("WHERE and HAVING conjunction failed", err)
	}
	request.Filters = []datatable.Filter{{Op: datatable.Any, Children: request.Filters}}
	if _, err := reporting.Totals.Query(scope.Context(), f.manager, authority, request); err == nil {
		t.Fatal("mixed WHERE/HAVING OR accepted")
	}
	artifact, err := reporting.Totals.Export(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Greater, Column: "total", Values: []string{"10"}}}}, datatable.ExportOptions{Format: datatable.CSV})
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	data, err := io.ReadAll(artifact)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || artifact.Rows() != 1 || len(rows) != 2 || rows[1][0] != f.members[0].String() || rows[1][1] != "2" || rows[1][2] != "30" {
		t.Fatal("grouped export diverged from count/filter scope", rows, err)
	}
	if err := artifact.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(f.tempDir)
	if err != nil || len(files) != 0 {
		t.Fatal("consumer export did not release temporary storage", err)
	}
}
