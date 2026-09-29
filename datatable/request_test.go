package datatable

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestRequestDecodingRejectsAmbiguousAndUndeclaredJSON(t *testing.T) {
	for _, input := range []string{
		`{"unknown":1}`, `{"page":1,"page":2}`, `{"Page":1}`, `{"page":"1"}`,
		`{"sort":[{"column":"id","direction":"sideways"}]}`,
		`{"filters":[{"op":"sql","column":"id","values":["1"]}]}`,
		`{"filters":[{"op":"eq","column":"id","values":[1]}]}`,
		`{"search":"\ud800"}`, `{}` + `{}`, strings.Repeat(" ", MaxRequestBytes) + `{}`,
	} {
		if result, err := DecodeRequest(t.Context(), []byte(input)); err == nil || !reflect.DeepEqual(result, Request{}) {
			t.Fatal("invalid request decoded", err)
		}
	}
	result, err := DecodeRequest(t.Context(), []byte(`{"page":2,"size":10,"sort":[{"column":"name","direction":"asc"}],"filters":[{"op":"eq","column":"id","values":["42"]}]}`))
	if err != nil || result.Page != 2 || result.Size != 10 || len(result.Sort) != 1 || len(result.Filters) != 1 {
		t.Fatal("valid request lost typed structure", err)
	}
	if _, err := DecodeRequest(nil, []byte(`{}`)); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestRequestAllowlistBoundsAndStableOrdering(t *testing.T) {
	table := Define(reportSpec())
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	invalidRequests := []Request{
		{Page: -1}, {Size: -1}, {Page: math.MaxInt, Size: 2}, {Size: query.MaxPageSize + 1}, {Page: config.MaxOffset + 2, Size: 1},
		{Sort: []Sort{{Column: "name; SELECT 1", Direction: Ascending}}},
		{Sort: []Sort{{Column: "id", Direction: "DESC NULLS FIRST"}}},
		{Sort: []Sort{{Column: "id", Direction: Ascending}, {Column: "id", Direction: Descending}}},
		{Filters: []Filter{{Op: Equal, Column: "tenant_id", Values: []string{"2"}}}},
		{Filters: []Filter{{Op: Contains, Column: "id", Values: []string{"2"}}}},
		{Filters: []Filter{{Op: Equal, Column: "id", Values: []string{"02"}}}},
		{Filters: []Filter{{Op: Between, Column: "id", Values: []string{"1"}}}},
		{Filters: []Filter{{Op: In, Column: "id"}}},
		{Filters: []Filter{{Op: IsNull, Column: "id"}}},
		{Filters: []Filter{{Op: IsNull, Column: "note", Values: []string{"ignored"}}}},
		{Filters: []Filter{{Op: All}}},
		{Filters: []Filter{{Op: Not, Children: []Filter{{Op: IsNull, Column: "note"}, {Op: IsNull, Column: "note"}}}}},
		{Filters: []Filter{{Op: Any, Column: "id", Children: []Filter{{Op: IsNull, Column: "note"}}}}},
		{Search: strings.Repeat("x", MaxScalarBytes+1)},
		{Search: string([]byte{0xff})},
	}
	leaf := Filter{Op: Equal, Column: "id", Values: []string{"1"}}
	deep := leaf
	for range MaxFilterDepth + 2 {
		deep = Filter{Op: All, Children: []Filter{deep}}
	}
	invalidRequests = append(invalidRequests, Request{Filters: []Filter{deep}})
	wide := make([]Filter, MaxFilters+1)
	for i := range wide {
		wide[i] = leaf
	}
	invalidRequests = append(invalidRequests, Request{Filters: wide})
	cycle := Filter{Op: All}
	cycle.Children = []Filter{cycle}
	cycle.Children[0].Children = cycle.Children
	invalidRequests = append(invalidRequests, Request{Filters: []Filter{cycle}})
	for i, request := range invalidRequests {
		if _, err := table.definition.prepare(request, config); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsafe request passed preparation", i, err)
		}
	}
	prepared, err := table.definition.prepare(Request{Sort: []Sort{{Column: "name", Direction: Descending}}, Search: "Alpha %_!"}, config)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.page.Number != 1 || prepared.page.Size != DefaultPageSize {
		t.Fatal("default page contract changed")
	}
	source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := source.rows.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"tenant_id" = $1`, `"deleted_at" IS NULL`, `ILIKE $2 ESCAPE '!'`, `ORDER BY "report_rows"."name" DESC, "report_rows"."id" ASC`} {
		if !strings.Contains(statement.SQL(), want) {
			t.Fatal("scope, literal search or unique order lost", statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(7), "%Alpha !%!_!!%"}) {
		t.Fatal("client strings changed SQL binding", statement.Arguments())
	}
	counted, err := source.count.Compile()
	if err != nil || strings.Contains(counted.SQL(), "ORDER BY") || !strings.Contains(counted.SQL(), `ILIKE $2 ESCAPE '!'`) || !reflect.DeepEqual(counted.Arguments(), statement.Arguments()) {
		t.Fatal("count query kept the table ordering or lost its filters", counted.SQL(), err)
	}
}

func TestLikeRequiresExplicitOptIn(t *testing.T) {
	_, name, note, _ := reportFields()
	text := foundryhttp.StringQuery[string]()
	plain := Where(text, name)
	if info, err := plain.Description(); err != nil || slices.Contains(info.Operators, Like) || !slices.Contains(info.Operators, Contains) {
		t.Fatal("like was enabled without an explicit opt-in", info.Operators, err)
	}
	if plain.Restrict(Like).Validate() == nil {
		t.Fatal("restriction enabled a disabled pattern operator")
	}
	allowed := Where(text, name).AllowLike()
	if info, err := allowed.Description(); err != nil || !slices.Contains(info.Operators, Like) {
		t.Fatal("AllowLike did not enable like", err)
	}
	if info, err := NullableWhere(text, note).AllowLike().Restrict(Like, IsNull).Description(); err != nil || !reflect.DeepEqual(info.Operators, []Operator{IsNull, Like}) {
		t.Fatal("nullable like could not be restricted after opt-in", info.Operators, err)
	}
	id, _, _, _ := reportFields()
	if Where(foundryhttp.IntegerQuery[int64](), id).AllowLike().Validate() == nil {
		t.Fatal("like enabled on a source without pattern comparisons")
	}
	spec := reportSpec()
	table := Define(spec)
	if _, err := table.definition.prepare(Request{Filters: []Filter{{Op: Like, Column: "name", Values: []string{"A%"}}}}, DefaultConfig()); !errors.Is(err, fault.Invalid) {
		t.Fatal("request used an undeclared like filter", err)
	}
	spec.Columns[1] = DefineColumn[reportRecord](validation.DefineField("name", func(row ReportRow) string { return row.Name }), "reports.name").SortBy(name.Value()).FilterBy(Where(text, name).AllowLike()).Registration()
	table = Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	prepared, err := table.definition.prepare(Request{Filters: []Filter{{Op: Like, Column: "name", Values: []string{"A%"}}}}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if statement, err := source.rows.Compile(); err != nil || !strings.Contains(statement.SQL(), " LIKE $2") || !reflect.DeepEqual(statement.Arguments(), []any{int64(7), "A%"}) {
		t.Fatal("explicit like lost its pattern", err)
	}
}

func TestNegatedFiltersIncludeRowsWithNullOperands(t *testing.T) {
	table := Define(reportSpec())
	compile := func(filters ...Filter) string {
		t.Helper()
		prepared, err := table.definition.prepare(Request{Filters: filters}, DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
		if err != nil {
			t.Fatal(err)
		}
		statement, err := source.count.Compile()
		if err != nil {
			t.Fatal(err)
		}
		return statement.SQL()
	}
	eq := Filter{Op: Equal, Column: "note", Values: []string{"memo"}}
	for name, filter := range map[string]Filter{
		"ne":      {Op: NotEqual, Column: "note", Values: []string{"memo"}},
		"not_in":  {Op: NotIn, Column: "note", Values: []string{"memo", "x"}},
		"not eq":  {Op: Not, Children: []Filter{eq}},
		"not and": {Op: Not, Children: []Filter{{Op: All, Children: []Filter{eq, {Op: Equal, Column: "id", Values: []string{"1"}}}}}},
		"not or":  {Op: Not, Children: []Filter{{Op: Any, Children: []Filter{eq, {Op: Contains, Column: "note", Values: []string{"x"}}}}}},
	} {
		if sql := compile(filter); !strings.Contains(sql, `"note" IS NULL`) {
			t.Fatal("negation dropped NULL rows", name, sql)
		}
	}
	for name, filter := range map[string]Filter{
		"not null test": {Op: Not, Children: []Filter{{Op: IsNull, Column: "note"}}},
		"non-null ne":   {Op: NotEqual, Column: "id", Values: []string{"1"}},
		"double not":    {Op: Not, Children: []Filter{{Op: Not, Children: []Filter{eq}}}},
	} {
		if sql := compile(filter); strings.Contains(sql, `"note" IS NULL OR`) || strings.Contains(sql, `OR "report_rows"."note" IS NULL`) {
			t.Fatal("exact negation gained a NULL branch", name, sql)
		}
	}
}

type countingIntegerCodec struct{ calls *int }

func (c countingIntegerCodec) Parse(text string) (int64, error) {
	*c.calls++
	return foundryhttp.IntegerQuery[int64]().Parse(text)
}
func (c countingIntegerCodec) Format(v int64) (string, error) {
	return foundryhttp.IntegerQuery[int64]().Format(v)
}

func TestRequestValidatesEntireShapeBeforeCallingScalarExtensions(t *testing.T) {
	spec := reportSpec()
	calls := 0
	codec := foundryhttp.DescribeURL[int64](countingIntegerCodec{&calls}, contract.DefineScalar[int64](contract.Type{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true}))
	id, _, _, _ := reportFields()
	spec.Columns[0] = DefineColumn[reportRecord](validation.DefineField("id", func(row ReportRow) int64 { return row.ID }), "reports.id").FilterBy(Where(codec, id)).Registration()
	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	request := Request{Filters: []Filter{{Op: Equal, Column: "id", Values: []string{"1"}}, {Op: Equal, Column: "missing", Values: []string{"2"}}}}
	if _, err := table.definition.prepare(request, DefaultConfig()); err == nil || calls != 0 {
		t.Fatal("scalar callbacks ran before structural rejection", calls, err)
	}
	request.Filters = request.Filters[:1]
	if _, err := table.definition.prepare(request, DefaultConfig()); err != nil || calls != 1 {
		t.Fatal("valid scalar did not parse exactly once", calls, err)
	}
}

func TestFilterGroupsPreserveWhereAndHavingPhases(t *testing.T) {
	spec := reportSpec()
	spec.Filters = []FilterRegistration[reportRecord]{DefineFilter("count", "reports.count", Having(foundryhttp.IntegerQuery[int64](), query.Count[reportRecord]()))}
	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	where := Filter{Op: Between, Column: "id", Values: []string{"1", "3"}}
	having := Filter{Op: Greater, Column: "count", Values: []string{"2"}}
	for _, group := range []Filter{{Op: Any, Children: []Filter{where, having}}, {Op: Not, Children: []Filter{{Op: All, Children: []Filter{where, having}}}}} {
		if _, err := table.definition.prepare(Request{Filters: []Filter{group}}, DefaultConfig()); err == nil {
			t.Fatal("mixed-phase boolean group accepted")
		}
	}
	prepared, err := table.definition.prepare(Request{Filters: []Filter{{Op: All, Children: []Filter{where, having}}}}, DefaultConfig())
	if err != nil || len(prepared.condition.where) != 2 || len(prepared.condition.having) != 1 {
		t.Fatal("AND did not preserve both phases", err)
	}
	prepared, err = table.definition.prepare(Request{Filters: []Filter{{Op: Any, Children: []Filter{where, {Op: Equal, Column: "id", Values: []string{"8"}}}}}}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := source.rows.Compile()
	if err != nil || !strings.Contains(statement.SQL(), " OR ") || !strings.Contains(statement.SQL(), " AND ") || !reflect.DeepEqual(statement.Arguments(), []any{int64(7), int64(1), int64(3), int64(8)}) {
		t.Fatal("OR did not keep BETWEEN endpoints together", statement.SQL(), err)
	}
}

func TestAuthorizationPrecedesSourceAndIsActionSpecific(t *testing.T) {
	spec := reportSpec()
	sourceCalls := 0
	var actions []Action
	base := spec.Source
	spec.Source = func(ctx context.Context, a reportActor) (query.ProjectionQuery[reportRecord, ReportRow], error) {
		sourceCalls++
		return base(ctx, a)
	}
	spec.Authorize = func(_ context.Context, a reportActor, action Action) error {
		actions = append(actions, action)
		if !a.Allowed {
			return auth.Forbidden
		}
		return nil
	}
	table := Define(spec)
	prepared, err := table.definition.prepare(Request{}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []Action{QueryAction, ExportAction} {
		if _, err := table.definition.scoped(t.Context(), reportActor{}, action, prepared); !errors.Is(err, auth.Forbidden) {
			t.Fatal("unauthorized source admitted", err)
		}
	}
	if sourceCalls != 0 || !reflect.DeepEqual(actions, []Action{QueryAction, ExportAction}) {
		t.Fatal("authority did not precede source")
	}
	if _, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, ExportAction, prepared); err != nil || sourceCalls != 1 {
		t.Fatal("authorized source failed", err)
	}
}

func FuzzDatatableRequestBounds(f *testing.F) {
	for _, seed := range []string{`{}`, `{"search":"hello"}`, `{"filters":[{"op":"is_null","column":"note"}]}`, `{"sort":[{"column":"id","direction":"asc"}]}`} {
		f.Add(seed)
	}
	table := Define(reportSpec())
	if err := table.Validate(); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > MaxRequestBytes+1 {
			t.Skip()
		}
		request, err := DecodeRequest(t.Context(), []byte(input))
		if err != nil {
			return
		}
		prepared, err := table.definition.prepare(request, DefaultConfig())
		if err != nil {
			return
		}
		if _, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared); err != nil {
			t.Fatal("validated request produced invalid SQL", err)
		}
	})
}
