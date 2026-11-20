package goway_test

import (
	"testing"

	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	ormrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"
	"github.com/weiloon1234/Foundry-Go/value"

	"foundry.test/goway/models"
	"foundry.test/goway/models/scalars"
	"foundry.test/goway/models/sensitive"
)

// TestUnifiedPositiveSurface verifies the positive compile controls for the
// unified ORM ColumnHandle surface, explicit type-argument prefixes, encrypted
// audit/change helpers, and all 9 shipping datatable Spec.Columns constructors.
func TestUnifiedPositiveSurface(t *testing.T) {
	u := models.Users
	s := scalars.Records
	v := sensitive.Vaults

	// Core unified operators, intervals, encrypted writes/nulls/audit/changes.
	_ = orm.Eq(u.Email, "ada@example.test")
	_ = orm.Ne(u.Email, "other@example.test")
	_ = orm.In(u.Status, models.StatusActive, models.StatusDisabled)
	_ = orm.Asc(u.Status)
	_ = orm.Desc(u.Age)
	_ = orm.Gt(s.Period, temporal.Days(1))
	_ = orm.Between(s.Period, temporal.Days(1), temporal.Days(7))
	_ = orm.Like(u.Email, "%@example.test")
	_ = orm.Set(u.Email, "ada@example.test")
	_ = orm.SetNull(u.Nickname)
	_ = orm.IsNull(u.Nickname)
	_ = orm.IsNotNull(u.Nickname)
	_ = orm.Increment(u.Age, 1)
	_ = orm.Decrement(u.Age, 1)

	secret := encrypted.NewText("secret")
	_ = orm.Set(v.Token, secret)
	_ = orm.SetIfPresent(nil, v.Token, value.Set(secret))
	_ = orm.Set(v.Backup, secret)
	_ = orm.SetNull(v.Backup)
	_ = orm.IsNull(v.Backup)
	_ = orm.IsNotNull(v.Backup)
	_ = orm.AuditField(v.Token, auditrecord.Redact)
	_ = orm.AuditField(v.Backup, auditrecord.Redact)
	var history auditrecord.Model[sensitive.Vault, model.ID[sensitive.Vault]]
	_, _ = orm.AuditRead(history, v.Token)
	_, _ = orm.AuditReadNullable(history, v.Backup)
	var ch orm.Changes[sensitive.Vault]
	_, _ = orm.ChangeOf(ch, v.Token)
	_, _ = orm.NullableChangeOf(ch, v.Backup)

	// Explicit type-argument prefixes preserved.
	var _ = orm.Eq[models.User, string, string, string, orm.RequiredTextKind, orm.OrdinaryRole]
	_ = expr.Sum[models.User, int, int](u.Age)
	var slot validation.Slot[model.ID[models.User]]
	_ = dbvalidation.UniqueIgnoring[models.User, string, model.ID[models.User]](nil, u.Query(), u.Email, u.Key(), slot)
	_ = modelbinding.ByColumn(nil, u.Query(), u.Email, func(p string) string { return p })

	// Relations, expressions, and row mappings take handles directly.
	_ = orm.JoinKeys(u.ID, models.Orders.BuyerID)
	_ = orm.ConflictKey(u.Email)
	_ = ormrows.MapInsert(u.Email, expr.Value(u.Email))
	_ = ormrows.MapUpdate(u.Email, expr.Value(u.Email))
	_ = ormrows.OnEqual(expr.Value(u.ID), expr.Value(models.Orders.BuyerID))

	// All 9 shipping datatable Spec.Columns short-name constructors + ColumnFeature.
	type userRow struct {
		Email    string
		Age      int
		Status   models.Status
		Nickname value.Nullable[string]
		Birthday value.Nullable[temporal.Date]
	}
	type scalarRow struct {
		Instant     temporal.DateTime
		MaybeMillis value.Nullable[int64]
	}
	type boolRow struct {
		Enabled bool
	}
	type optEnumRow struct {
		OptStatus value.Nullable[models.Status]
	}
	var optEnumCol orm.NullableEnum[models.User, models.Status]
	statusCodec := foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor())
	dateCodec := foundryhttp.TextQuery[temporal.Date, *temporal.Date]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()

	_ = []datatable.ColumnRegistration[models.User, userRow]{
		datatable.Text(validation.DefineField("email", func(r userRow) string { return r.Email }), "u.email", u.Email, datatable.NoSearch),
		datatable.Number(validation.DefineField("age", func(r userRow) int { return r.Age }), "u.age", u.Age, foundryhttp.IntegerQuery[int](), datatable.NoSort),
		datatable.Enum(validation.DefineField("status", func(r userRow) models.Status { return r.Status }), "u.status", u.Status, statusCodec, datatable.NoFilter),
		datatable.NullableText(validation.DefineField("nickname", func(r userRow) value.Nullable[string] { return r.Nickname }), "u.nickname", u.Nickname, datatable.NoExport),
		datatable.NullableTime(validation.DefineField("birthday", func(r userRow) value.Nullable[temporal.Date] { return r.Birthday }), "u.birthday", u.Birthday, dateCodec),
	}
	_ = []datatable.ColumnRegistration[scalars.Record, scalarRow]{
		datatable.Time(validation.DefineField("instant", func(r scalarRow) temporal.DateTime { return r.Instant }), "s.instant", s.Instant, dtCodec),
		datatable.NullableNumber(validation.DefineField("maybe_millis", func(r scalarRow) value.Nullable[int64] { return r.MaybeMillis }), "s.maybe_millis", s.MaybeMillis, foundryhttp.IntegerQuery[int64]()),
	}
	_ = datatable.Bool(validation.DefineField("enabled", func(r boolRow) bool { return r.Enabled }), "w.enabled", models.WriteRecords.Enabled)
	_ = datatable.NullableEnum(validation.DefineField("opt_status", func(r optEnumRow) value.Nullable[models.Status] { return r.OptStatus }), "u.opt_status", optEnumCol, statusCodec)
}
