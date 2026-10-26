package migration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/validation"
	validationdb "github.com/weiloon1234/Foundry-Go/validation/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// migration:pilot-declarations
type Admin struct {
	ID          int64
	TenantID    int64
	Username    string
	Email       value.Nullable[string]
	CountryCode countries.Code
	DeletedAt   value.Nullable[time.Time]
}
type AdminTable struct {
	orm.Table[Admin, int64]
	TenantID    orm.Number[Admin, int64]
	Username    orm.Text[Admin, string]
	Email       orm.NullableText[Admin, string]
	CountryCode orm.Column[Admin, countries.Code]
	DeletedAt   orm.SoftDelete[Admin, time.Time]
}

var Admins = orm.Define("admins", func(b orm.Builder[Admin], a *Admin) AdminTable {
	return AdminTable{orm.TableOf(b, &a.ID), orm.NumberOf(b, &a.TenantID),
		orm.TextOf(b, &a.Username), orm.NullableTextOf(b, &a.Email),
		orm.ColumnOf(b, &a.CountryCode), orm.SoftDeleteOf(b, &a.DeletedAt)}
})

// Keep this DTO's original package, name, field types and JSON tags.
type AdminRow struct {
	ID       int64                  `json:"id"`
	Username string                 `json:"username"`
	Email    value.Nullable[string] `json:"email"`
}
type AdminRowColumns struct {
	rows.Row[AdminRow]
	ID       rows.Field[AdminRow, int64]
	Username rows.Field[AdminRow, string]
	Email    rows.Field[AdminRow, value.Nullable[string]]
}

var AdminRows = rows.Define(func(b rows.Builder[AdminRow], r *AdminRow) AdminRowColumns {
	return AdminRowColumns{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Username), rows.NullableTextOf(b, &r.Email)}
})

// migration:end

// migration:pilot-columns
func adminColumns() []datatable.ColumnRegistration[Admin, AdminRow] {
	text := foundryhttp.StringQuery[string]()
	username := validation.DefineField("username", func(r AdminRow) string { return r.Username })
	email := validation.DefineField("email", func(r AdminRow) value.Nullable[string] { return r.Email })
	return []datatable.ColumnRegistration[Admin, AdminRow]{
		datatable.DefineColumn[Admin](username, "admins.fields.username").
			SortBy(expr.Row(expr.Value(Admins.Username)).Value()).
			FilterBy(datatable.Where(text, expr.TextRow(Admins.Username))).
			Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
		datatable.DefineColumn[Admin](email, "admins.fields.email").
			SortBy(expr.Row(expr.Value(Admins.Email)).Value()).
			FilterBy(datatable.NullableWhere(text, expr.TextNullableRow(Admins.Email))).
			Searchable().ExportAs(datatable.NullableCell(text)).Registration(),
	}
}

// migration:end

// migration:pilot-source
func adminSource(tenantID int64, archived bool) rows.Query[Admin, AdminRow] {
	source := Admins.Query().Where(Admins.TenantID.Eq(tenantID))
	if archived {
		source = source.OnlyTrashed()
	}
	return rows.Select(rows.From(source), AdminRows,
		rows.Map(AdminRows.ID, expr.Value(Admins.Key())),
		rows.Map(AdminRows.Username, expr.Value(Admins.Username)),
		rows.Map(AdminRows.Email, expr.Value(Admins.Email)))
}

func adminTable[A any](spec datatable.Spec[Admin, AdminRow, A], tenant func(context.Context, A) (int64, error), archived bool) datatable.Spec[Admin, AdminRow, A] {
	// Retain the caller's Row descriptor, Authorize, ID, labels and export policy.
	spec.Columns = adminColumns()
	spec.Stable = []expr.ProjectionOrder[Admin]{expr.Value(Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[Admin, AdminRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[Admin, AdminRow]{}, err
		}
		return adminSource(id, archived), nil
	}
	return spec
}

// migration:end

// migration:pilot-left-join-adapters
type auditAdminSide struct{}
type auditCountrySide struct{}
type AuditScope = rows.Left[rows.Alias[auditAdminSide, Admin], rows.Alias[auditCountrySide, countries.Country]]
type AuditRow struct {
	ID       int64                            `json:"id"`
	Country  value.Nullable[string]           `json:"country"`
	Status   value.Nullable[countries.Status] `json:"status"`
	Latitude value.Nullable[float64]          `json:"latitude"`
}
type AuditColumns struct {
	rows.Row[AuditRow]
	ID       rows.Field[AuditRow, int64]
	Country  rows.Field[AuditRow, value.Nullable[string]]
	Status   rows.Field[AuditRow, value.Nullable[countries.Status]]
	Latitude rows.Field[AuditRow, value.Nullable[float64]]
}

var AuditRows = rows.Define(func(b rows.Builder[AuditRow], r *AuditRow) AuditColumns {
	return AuditColumns{
		b.Row(),
		rows.NumberOf(b, &r.ID),
		rows.NullableTextOf(b, &r.Country),
		rows.NullableEnumOf(b, &r.Status),
		rows.NullableNumberOf(b, &r.Latitude),
	}
})

func auditTable[A any](spec datatable.Spec[AuditScope, AuditRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[AuditScope, AuditRow, A] {
	a := rows.As[auditAdminSide](Admins.Query(), "a")
	c := rows.As[auditCountrySide](countries.Countries.Query(), "c")
	as, cs := rows.Scope(a), rows.Scope(c)
	joined := rows.LeftJoin(a, c, rows.On(expr.At(as, Admins.CountryCode), expr.At(cs, countries.Countries.ISO2)))
	admin := rows.LeftScope(joined, as)
	country := rows.NullableRightScope(joined, cs)
	correlateName := func() rows.CorrelatedScalar[AuditScope, countries.Country, string] {
		q := countries.Countries.Query()
		correlation := rows.Correlate(joined, rows.From(q))
		outer := rows.OuterScope(correlation, admin)
		inner := rows.InnerScope(correlation, rows.ModelScope(q))
		return rows.SelectCorrelatedScalar(correlation, expr.Row(expr.At(inner, countries.Countries.Name)).Value()).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.At(inner, countries.Countries.ISO2).Asc()).Limit(1)
	}
	sortedCountry := expr.Row(rows.CorrelatedScalarRowQuery(correlateName())).Value()
	statusFilter := datatable.NullableWhere(
		foundryhttp.EnumQuery[countries.Status, *countries.Status](countries.EnabledStatus.EnumDescriptor()),
		expr.CompareNullableRow(expr.AtNullable(country, countries.Countries.Status)))
	latitudeFilter := datatable.NullableWhere(
		foundryhttp.FloatQuery[float64](),
		expr.OrderNullableRow(expr.AtNullableValue(country, countries.Countries.Latitude)))
	base := rows.Select(joined, AuditRows,
		rows.Map(AuditRows.ID, expr.At(admin, Admins.Key())),
		rows.MapSelected(AuditRows.Country, sortedCountry),
		rows.Map(AuditRows.Status, expr.AtNullable(country, countries.Countries.Status)),
		rows.Map(AuditRows.Latitude, expr.AtNullableValue(country, countries.Countries.Latitude)))
	spec.Columns = []datatable.ColumnRegistration[AuditScope, AuditRow]{
		datatable.DefineColumn[AuditScope](validation.DefineField("country", func(r AuditRow) value.Nullable[string] { return r.Country }), "audit.country").
			SortBy(sortedCountry).Registration(),
		datatable.DefineColumn[AuditScope](validation.DefineField("status", func(r AuditRow) value.Nullable[countries.Status] { return r.Status }), "audit.status").
			FilterBy(statusFilter).Registration(),
		datatable.DefineColumn[AuditScope](validation.DefineField("latitude", func(r AuditRow) value.Nullable[float64] { return r.Latitude }), "audit.latitude").
			FilterBy(latitudeFilter).Registration(),
	}
	spec.Stable = []expr.ProjectionOrder[AuditScope]{expr.At(admin, Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[AuditScope, AuditRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[AuditScope, AuditRow]{}, err
		}
		return base.Where(expr.At(admin, Admins.TenantID).Eq(id)), nil
	}
	return spec
}

// migration:end

// migration:pilot-filter-families
func orderGroupFilters() (datatable.FilterSource[models.Order, int64], datatable.FilterSource[models.Order, value.Nullable[decimal.Decimal]]) {
	count := expr.Count[models.Order]()
	total := expr.Sum(models.Orders.TotalCents)
	return datatable.Having(foundryhttp.IntegerQuery[int64](), count),
		datatable.NullableHaving(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), total)
}

func relatedOrderFilter() datatable.FilterSource[models.User, int64] {
	return datatable.Related(
		datatable.Where(foundryhttp.IntegerQuery[int64](), expr.OrderRow(expr.Value(models.Orders.TotalCents))),
		func(p expr.Predicate[models.Order]) expr.Predicate[models.User] {
			return expr.Has(models.UserRelations.Orders, p)
		})
}

func statusFilter() datatable.FilterSource[models.User, models.Status] {
	return datatable.Where(foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()),
		expr.CompareRow(expr.Value(models.Users.Status)))
}

// migration:end

// migration:pilot-custom-filter
type officialNameFilter[S any] struct {
	exists func(orm.Predicate[countries.Country]) expr.Predicate[S]
}

func (f officialNameFilter[S]) Eq(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.Eq(v))
}
func (f officialNameFilter[S]) Ne(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.Ne(v))
}
func (f officialNameFilter[S]) In(v ...string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.In(v...))
}
func (f officialNameFilter[S]) IContains(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.IContains(v))
}
func (f officialNameFilter[S]) IsNull() expr.Predicate[S] {
	return expr.Not(f.IsNotNull())
}
func (f officialNameFilter[S]) IsNotNull() expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.IsNotNull())
}

// migration:end

// migration:pilot-joined-schema
type adminSide struct{}
type countrySide struct{}
type DirectoryScope = rows.Inner[rows.Alias[adminSide, Admin], rows.Alias[countrySide, countries.Country]]
type DirectoryRow struct {
	ID           int64                  `json:"id"`
	Country      string                 `json:"country"`
	OfficialName value.Nullable[string] `json:"officialName"`
}
type DirectoryColumns struct {
	rows.Row[DirectoryRow]
	ID           rows.Field[DirectoryRow, int64]
	Country      rows.Field[DirectoryRow, string]
	OfficialName rows.Field[DirectoryRow, value.Nullable[string]]
}

var DirectoryRows = rows.Define(func(b rows.Builder[DirectoryRow], r *DirectoryRow) DirectoryColumns {
	return DirectoryColumns{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Country), rows.NullableTextOf(b, &r.OfficialName)}
})

// migration:end

// migration:pilot-joined-source
func directoryTable[A any](spec datatable.Spec[DirectoryScope, DirectoryRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[DirectoryScope, DirectoryRow, A] {
	a := rows.As[adminSide](Admins.Query(), "a")
	c := rows.As[countrySide](countries.Countries.Query(), "c")
	as, cs := rows.Scope(a), rows.Scope(c)
	joined := rows.InnerJoin(a, c, rows.On(expr.At(as, Admins.CountryCode), expr.At(cs, countries.Countries.ISO2)))
	admin, country := rows.LeftScope(joined, as), rows.RightScope(joined, cs)
	correlate := func(ps ...orm.Predicate[countries.Country]) rows.CorrelatedScalar[DirectoryScope, countries.Country, value.Nullable[string]] {
		q := countries.Countries.Query().Where(ps...)
		correlation := rows.Correlate(joined, rows.From(q))
		outer := rows.OuterScope(correlation, admin)
		inner := rows.InnerScope(correlation, rows.ModelScope(q))
		return rows.SelectCorrelatedScalar(correlation, expr.Row(expr.At(inner, countries.Countries.OfficialName)).Value()).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.At(inner, countries.Countries.ISO2).Asc()).Limit(1)
	}
	selected := expr.Row(rows.CorrelatedScalarNullableRowQuery(correlate())).Value()
	filter := officialNameFilter[DirectoryScope]{exists: func(p orm.Predicate[countries.Country]) expr.Predicate[DirectoryScope] {
		return correlate(p).Exists()
	}}
	base := rows.Select(joined, DirectoryRows,
		rows.Map(DirectoryRows.ID, expr.At(admin, Admins.Key())),
		rows.Map(DirectoryRows.Country, expr.At(country, countries.Countries.Name)),
		rows.MapSelected(DirectoryRows.OfficialName, selected))
	text := foundryhttp.StringQuery[string]()
	spec.Columns = []datatable.ColumnRegistration[DirectoryScope, DirectoryRow]{
		datatable.DefineColumn[DirectoryScope](validation.DefineField("country", func(r DirectoryRow) string { return r.Country }), "admins.country").
			SortBy(expr.Row(expr.At(country, countries.Countries.Name)).Value()).
			FilterBy(datatable.Where(text, expr.TextRowAt(country, countries.Countries.Name))).Registration(),
		datatable.DefineColumn[DirectoryScope](validation.DefineField("officialName", func(r DirectoryRow) value.Nullable[string] { return r.OfficialName }), "admins.officialName").
			SortBy(selected).FilterBy(datatable.NullableWhere(text, filter)).Registration(),
	}
	spec.Stable = []expr.ProjectionOrder[DirectoryScope]{expr.At(admin, Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[DirectoryScope, DirectoryRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[DirectoryScope, DirectoryRow]{}, err
		}
		return base.Where(expr.At(admin, Admins.TenantID).Eq(id)), nil
	}
	return spec
}

// migration:end

// migration:pilot-countries
func countryRead(ctx context.Context, db *database.DB, code countries.Code) (value.Optional[countries.Country], error) {
	country, err := countries.Countries.Find(ctx, db, code)
	if err != nil || !country.IsSet() {
		return country, err
	}
	f := countries.Countries
	_, err = f.Query().Where(f.Status.Eq(countries.EnabledStatus)).
		OrderBy(f.Name.Asc(), f.ISO2.Asc()).Limit(countries.MaxCountries).All(ctx, db.Primary())
	return country, err
}

func countryWrite(ctx context.Context, tx *database.Tx, code countries.Code, rate decimal.Decimal) error {
	f := countries.Countries
	_, err := f.Query().Where(f.ISO2.Eq(code)).ForUpdate().First(ctx, tx)
	if err != nil {
		return err
	}
	_, err = f.Update(ctx, tx, code, f.Status.Set(countries.EnabledStatus), f.ConversionRate.Set(rate))
	return err
}

// migration:end

// migration:pilot-lookup
type canonical struct {
	validation.Lookup[string]
}

func (c canonical) Exists(ctx context.Context, v string) (bool, error) {
	return c.Lookup.Exists(ctx, strings.ToLower(v))
}

func UniqueUsername[M any](executor database.Executor, source orm.Query[M], column orm.KeyColumn[M, string], username validation.Rule[string]) validation.Rule[string] {
	return validation.Bail(username, validation.Unique(canonical{validationdb.Lookup(executor, source, column)}))
}

func adminUsernameRule(tx *database.Tx, username validation.Rule[string]) validation.Rule[string] {
	return UniqueUsername(tx, Admins.Query().WithTrashed(), Admins.Username, username)
}

// migration:end

// migration:pilot-recipe-locked-find
func requireAdminForUpdate(ctx context.Context, tx *database.Tx, id int64) (Admin, error) {
	return Admins.Query().Where(Admins.Key().Eq(id)).ForUpdate().Require(ctx, tx)
}

// migration:end

// migration:pilot-recipe-model-identity
var adminIdentity = orm.IdentityOf(Admins.Table, Admins.Key())

func (a Admin) FoundryReference() model.Reference[Admin, int64] {
	return adminIdentity.ReferenceOf(a)
}

func (a Admin) FoundryIdentity() (model.Identity, error) {
	return a.FoundryReference().Identity()
}

// migration:end

// migration:pilot-recipe-datatable-test
func isolatedDatatableConfig(scope *postgres.Scope) datatable.Config {
	config := datatable.DefaultConfig()
	config.Schema = scope.Schema()
	return config
}

// migration:end

// migration:pilot-recipe-optional-insert
func insertAdmin(ctx context.Context, tx *database.Tx, tenantID int64, username string, email value.Optional[string]) (Admin, error) {
	assignments := []orm.Assignment[Admin]{
		Admins.TenantID.Set(tenantID),
		Admins.Username.Set(username),
	}
	if v, ok := email.Get(); ok {
		assignments = append(assignments, Admins.Email.Set(v))
	}
	return Admins.Insert(ctx, tx, assignments...)
}

// migration:end

func TestPilotDeclarationsAndSources(t *testing.T) {
	if err := orm.Check(Admins.Table, countries.Countries.Table); err != nil {
		t.Fatal(err)
	}
	for _, archived := range []bool{false, true} {
		statement, err := rows.CompileRead(rows.Read(adminSource(17, archived)))
		if err != nil {
			t.Fatal(err)
		}
		want := "IS NULL"
		if archived {
			want = "IS NOT NULL"
		}
		if !strings.Contains(statement.SQL(), "\"deleted_at\" "+want) || len(statement.Arguments()) != 1 || statement.Arguments()[0] != int64(17) {
			t.Fatal("source lost visibility or tenant", statement.SQL(), statement.Arguments())
		}
	}
	spec := directoryTable(datatable.Spec[DirectoryScope, DirectoryRow, struct{}]{}, func(context.Context, struct{}) (int64, error) { return 17, nil })
	q, err := spec.Source(t.Context(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	statement, err := rows.CompileRead(rows.Read(q))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"INNER JOIN", "SELECT", "LIMIT", "tenant_id", "deleted_at"} {
		if !strings.Contains(statement.SQL(), part) {
			t.Fatal("missing joined/correlated clause", part, statement.SQL())
		}
	}
	auditSpec := auditTable(datatable.Spec[AuditScope, AuditRow, struct{}]{}, func(context.Context, struct{}) (int64, error) { return 17, nil })
	aq, err := auditSpec.Source(t.Context(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	astatement, err := rows.CompileRead(rows.Read(aq))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"LEFT JOIN", "SELECT", "LIMIT", "tenant_id", "deleted_at"} {
		if !strings.Contains(astatement.SQL(), part) {
			t.Fatal("missing left-joined/correlated clause", part, astatement.SQL())
		}
	}
	admin := Admin{ID: 17}
	if admin.FoundryReference().Key() != 17 {
		t.Fatal("unexpected reference key")
	}
	if ident, err := admin.FoundryIdentity(); err != nil || ident.IsZero() {
		t.Fatal("unexpected identity", err)
	}
	if cfg := isolatedDatatableConfig(nil); cfg.Schema != "" {
		t.Fatal("unexpected datatable test schema")
	}
	count, total := orderGroupFilters()
	for _, err := range []error{count.Validate(), total.Validate(), relatedOrderFilter().Validate(), statusFilter().Validate()} {
		if err != nil {
			t.Fatal(err)
		}
	}
}
