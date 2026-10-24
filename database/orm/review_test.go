package orm

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type keyStatus string

func (keyStatus) EnumDescriptor() enum.Descriptor[keyStatus] {
	return enum.Describe("ormtest", "Status", enum.Case[keyStatus]{Name: "Active", Value: "active"})
}

func TestDerivedKeyCapabilities(t *testing.T) {
	type CountryCode string
	type country struct{ Code CountryCode }
	table := Define("countries", func(b Builder[country], c *country) Table[country, CountryCode] { return TableOf(b, &c.Code) })
	text := TextKey(table.Key())
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	s, err := Compile(table.Query().Where(text.StartsWith("M")))
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{"M%"}) {
		t.Fatalf("text key: %s %v %v", s.SQL(), s.Arguments(), err)
	}
	type integer struct{ ID int64 }
	ints := Define("integers", func(b Builder[integer], m *integer) Table[integer, int64] { return TableOf(b, &m.ID) })
	number := NumberKey(ints.Key())
	if number.number.core != ints.Key().key.core {
		t.Fatal("derived core changed")
	}
	for _, v := range []int64{0, 5} {
		a, _ := ints.Key().Set(v).core.Bind()
		b, _ := number.Set(v).core.Bind()
		if a != b {
			t.Fatal("assignment differs")
		}
	}
	if _, err := Compile(ints.Query().Where(number.Gt(1), number.Between(2, 4))); err != nil {
		t.Fatal(err)
	}
	type enumerated struct{ ID keyStatus }
	enums := Define("enums", func(b Builder[enumerated], m *enumerated) Table[enumerated, keyStatus] { return TableOf(b, &m.ID) })
	if err := Check(enums); err != nil {
		t.Fatal(err)
	}
	for _, column := range []Column[enumerated, keyStatus]{{column: enums.Key().ormColumn(nil)}, {column: TextKey(enums.Key()).ormColumn(nil)}} {
		if _, err := Compile(enums.Query().Where(column.Eq("invalid"))); err == nil {
			t.Fatal("invalid enum predicate accepted")
		}
		if _, err := column.Set("invalid").core.Bind(); err == nil {
			t.Fatal("invalid enum assignment accepted")
		}
	}
	duplicate := Define("integers", func(b Builder[integer], m *integer) Table[integer, int64] {
		table := TableOf(b, &m.ID)
		NumberOf(b, &m.ID)
		return table
	})
	if err := Check(duplicate); err == nil || !strings.Contains(err.Error(), "only key binding") {
		t.Fatalf("duplicate key: %v", err)
	}
}
func TestPrimaryOwnershipAndSensitivity(t *testing.T) {
	type owner struct{ ID model.ID[record] }
	foreign := Define("foreign", func(b Builder[owner], m *owner) Table[owner, model.ID[record]] { return TableOf(b, &m.ID) })
	if err := Check(foreign); err == nil || !strings.Contains(err.Error(), "must belong to this model") {
		t.Fatalf("foreign ID: %v", err)
	}
	type secret struct{ ID string }
	sensitive := Define("secrets", func(b Builder[secret], m *secret) Table[secret, string] {
		return TableOf(b, &m.ID, Codec(codec.String[string]().WithSensitiveValues()))
	})
	if err := Check(sensitive); err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("sensitive key: %v", err)
	}
}
func TestCodecMetadataAndDefinedFrameworkType(t *testing.T) {
	type buffer []byte
	adapted := selectedCodec([]ColumnOption[buffer]{Codec(codec.Adapt(codec.Bytes[[]byte]().WithSensitiveValues().WithEquality(bytes.Equal), func(v buffer) []byte { return []byte(v) }, func(v []byte) buffer { return buffer(v) }))})
	original := buffer{1, 2}
	copy := adapted.Clone(original)
	copy[0] = 9
	equal, defined := adapted.Equal(original, buffer{1, 2})
	if original[0] != 1 || !equal || !defined || !adapted.SensitiveValues() {
		t.Fatal("codec metadata lost")
	}
	type UserID model.ID[record]
	type holder struct {
		ID     int
		UserID UserID
	}
	declare := func(custom bool) Table[holder, int] {
		return Define("holders", func(b Builder[holder], m *holder) Table[holder, int] {
			if custom {
				ColumnOf(b, &m.UserID, Codec(codec.Adapt(codec.ID[record](), func(v UserID) model.ID[record] { return model.ID[record](v) }, func(v model.ID[record]) UserID { return UserID(v) })))
			} else {
				ColumnOf(b, &m.UserID)
			}
			return TableOf(b, &m.ID)
		})
	}
	if err := Check(declare(false)); err == nil || !strings.Contains(err.Error(), "use orm.Codec") {
		t.Fatalf("actionable error missing: %v", err)
	}
	if err := Check(declare(true)); err != nil {
		t.Fatal(err)
	}
}
func TestDefineResultIntegrity(t *testing.T) {
	type item struct{ ID int }
	type schema struct{ Table[item, int] }
	good := Define("good", func(b Builder[item], m *item) schema { return schema{TableOf(b, &m.ID)} })
	cases := []struct {
		want    string
		declare func(Builder[item], *item) schema
	}{
		{"callback is missing", nil},
		{"TableOf was not called", func(Builder[item], *item) schema { return schema{} }},
		{"not assigned", func(b Builder[item], m *item) schema { TableOf(b, &m.ID); return schema{} }},
		{"another declaration", func(b Builder[item], m *item) schema { TableOf(b, &m.ID); return good }},
	}
	for _, test := range cases {
		t.Run(test.want, func(t *testing.T) {
			table := Define("bad", test.declare)
			err := Check(table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
			if _, err := Compile(table.Query()); err == nil {
				t.Fatal("invalid table compiled")
			}
		})
	}
	if err := Check(good); err != nil {
		t.Fatal("foreign result corrupted original", err)
	}
}
func TestStartupAdviceAndShadowing(t *testing.T) {
	type item struct {
		ID    int
		Query string
		slot  relation.One[record]
	}
	table := Define("items", func(b Builder[item], m *item) Table[item, int] { TextOf(b, &m.Query); return TableOf(b, &m.ID) })
	if err := Check(table); err == nil || !strings.Contains(err.Error(), "exported, non-embedded") || strings.Contains(err.Error(), "shadows") {
		t.Fatalf("model name/advice: %v", err)
	}
	type rel struct {
		ID       int
		Relation relation.One[record] `foundry:"column=bad"`
	}
	bad := Define("relations", func(b Builder[rel], m *rel) Table[rel, int] { return TableOf(b, &m.ID) })
	if err := Check(bad); err == nil || !strings.Contains(err.Error(), "relation slots") {
		t.Fatalf("relation tag: %v", err)
	}
	type key struct{ ID int }
	type shadow struct {
		Table[key, int]
		Query NumberLookupKey[key, int]
	}
	sh := Define("shadow", func(b Builder[key], m *key) shadow {
		table := TableOf(b, &m.ID)
		return shadow{table, NumberKey(table.Key())}
	})
	if err := Check(sh); err == nil || !strings.Contains(err.Error(), "shadows") {
		t.Fatalf("table shadow: %v", err)
	}
	type zero struct {
		ID    int
		Empty struct{}
	}
	z := Define("zero", func(b Builder[zero], m *zero) Table[zero, int] { ColumnOf(b, &m.Empty); return TableOf(b, &m.ID) })
	if err := Check(z); err == nil || !strings.Contains(err.Error(), "zero-sized") {
		t.Fatalf("zero-size: %v", err)
	}
}
func TestExplicitRolesAndOptOuts(t *testing.T) {
	type stamp struct {
		ID        int
		CreatedAt time.Time
		UpdatedAt time.Time
		DeletedAt value.Nullable[time.Time]
	}
	table := Define("stamps", func(b Builder[stamp], m *stamp) Table[stamp, int] {
		UpdatedAtOf(b, &m.CreatedAt)
		CreatedAtOf(b, &m.UpdatedAt)
		SoftDeleteOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	})
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	if ts := table.core.model.Timestamps; ts.Created != "updated_at" || ts.Updated != "created_at" {
		t.Fatal("conventions defeated explicit roles")
	}
	no := Define("stamps", func(b Builder[stamp], m *stamp) Table[stamp, int] {
		CreatedAtOf(b, &m.CreatedAt)
		UpdatedAtOf(b, &m.UpdatedAt)
		SoftDeleteOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	}, NoTimestamps())
	if err := Check(no); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("opt out: %v", err)
	}
	if unmanagedTimed(NoSoftDeletes()).core.model.SoftDelete != nil {
		t.Fatal("NoSoftDeletes ignored")
	}
	for _, scope := range []Query[timed]{timedRecords().Query().WithTrashed(), timedRecords().Query().OnlyTrashed(), timedRecords().Query().WithoutTrashed()} {
		if _, err := Compile(scope); err != nil {
			t.Fatal(err)
		}
	}
}
func TestOwnerlessAndJoinedErrors(t *testing.T) {
	r := records()
	sentinelA, sentinelB := errors.New("a"), errors.New("b")
	p := And(Predicate[record]{err: sentinelA}, Predicate[record]{err: sentinelB})
	if !errors.Is(p.err, sentinelA) || !errors.Is(p.err, sentinelB) {
		t.Fatal("predicate errors overwritten")
	}
	for _, q := range []Query[record]{r.Query().Where(Predicate[record]{}), r.Query().Where(And[record]()), r.Query().OrderBy(Order[record]{})} {
		if _, err := Compile(q); err == nil {
			t.Fatal("ownerless expression accepted")
		}
	}
}
func TestForeignIDNullableBytesAndEnums(t *testing.T) {
	type row struct {
		ID       int
		Foreign  model.ID[record]
		Nullable value.Nullable[model.ID[record]]
		Bytes    value.Nullable[[]byte]
		Status   keyStatus
		Maybe    value.Nullable[keyStatus]
	}
	var foreign Column[row, model.ID[record]]
	var nullable NullableColumn[row, model.ID[record]]
	var binary NullableColumn[row, []byte]
	var status Enum[row, keyStatus]
	var maybe NullableEnum[row, keyStatus]
	table := Define("rows", func(b Builder[row], m *row) Table[row, int] {
		foreign = IDOf(b, &m.Foreign)
		nullable = NullableIDOf(b, &m.Nullable)
		binary = NullableBytesOf(b, &m.Bytes)
		status = EnumOf(b, &m.Status)
		maybe = NullableEnumOf(b, &m.Maybe)
		return TableOf(b, &m.ID)
	})
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[record]("01890f47-1a00-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(table.Query().Where(foreign.Eq(id), nullable.Eq(id), maybe.IsNull())); err != nil {
		t.Fatal(err)
	}
	bytes := []byte{1, 2}
	a := binary.Set(bytes)
	bytes[0] = 9
	bound, err := a.core.Bind()
	if err != nil || bound.([]byte)[0] != 1 {
		t.Fatal("nullable bytes borrowed", err)
	}
	if _, err := status.Set("invalid").core.Bind(); err == nil {
		t.Fatal("invalid enum accepted")
	}
	if _, err := maybe.Set("invalid").core.Bind(); err == nil {
		t.Fatal("invalid nullable enum accepted")
	}
	var item row
	source := &testDriverRows{columns: []string{"id", "foreign", "nullable", "bytes", "status", "maybe"}, values: [][]driver.Value{{int64(1), id.String(), id.String(), []byte{3}, "active", nil}}}
	db := testDB(t, testConnector{columns: source.columns, values: source.values})
	items, err := table.Query().All(t.Context(), db)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	item = items[0]
	if item.Foreign != id {
		t.Fatal("foreign ID hydration")
	}
	if v, ok := item.Nullable.Get(); !ok || v != id {
		t.Fatal("nullable ID hydration")
	}
}

type numeric struct {
	ID       int
	Signed   int64
	Unsigned uint64
	Float    float64
	Decimal  decimal.Decimal
	Nullable value.Nullable[int64]
}
type numericTable struct {
	Table[numeric, int]
	Signed   Number[numeric, int64]
	Unsigned Number[numeric, uint64]
	Float    Number[numeric, float64]
	Decimal  Number[numeric, decimal.Decimal]
	Nullable NullableNumber[numeric, int64]
}

func numericRecords() numericTable {
	return Define("numbers", func(b Builder[numeric], m *numeric) numericTable {
		return numericTable{TableOf(b, &m.ID), NumberOf(b, &m.Signed), NumberOf(b, &m.Unsigned), NumberOf(b, &m.Float), NumberOf(b, &m.Decimal), NullableNumberOf(b, &m.Nullable)}
	})
}
func TestAdjustmentsRejectNonpositiveBeforeSQL(t *testing.T) {
	r := numericRecords()
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	assignments := []Assignment[numeric]{r.Signed.Increment(-1), r.Signed.Decrement(0), r.Unsigned.Increment(0), r.Float.Increment(-1), r.Float.Decrement(0), r.Float.Increment(math.NaN()), r.Float.Decrement(math.Inf(1)), r.Float.Increment(math.Inf(-1)), r.Float.Decrement(math.Copysign(0, -1)), r.Nullable.Decrement(-1)}
	for _, text := range []string{"0", "-1"} {
		d, err := decimal.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		assignments = append(assignments, r.Decimal.Increment(d), r.Decimal.Decrement(d))
	}
	for _, a := range assignments {
		if _, err := r.Update(t.Context(), db, 1, a); !errors.Is(err, fault.Invalid) {
			t.Fatalf("per-key amount: %v", err)
		}
		if _, err := r.Query().UpdateAll(t.Context(), db, a); !errors.Is(err, fault.Invalid) {
			t.Fatalf("set amount: %v", err)
		}
	}
	if len(statements) != 0 {
		t.Fatalf("invalid amounts sent SQL: %v", statements)
	}
}
func TestAtomicAdjustmentBounds(t *testing.T) {
	r := numericRecords()
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "signed", "unsigned", "float", "decimal", "nullable"}})
	_, err := r.Update(t.Context(), db, 1, r.Signed.Decrement(2, AtLeast(int64(0))))
	if !errors.Is(err, ErrBoundExceeded) || !errors.Is(err, fault.Conflict) || errors.Is(err, fault.Invalid) || errors.Is(err, database.NotFound) {
		t.Fatalf("bound error: %v", err)
	}
	if len(statements) != 1 || !strings.Contains(statements[0], `"signed" = "signed" - $1`) || !strings.Contains(statements[0], ` >= `) {
		t.Fatalf("guard SQL: %v", statements)
	}
	n, err := r.Query().UpdateAll(t.Context(), db, r.Float.Increment(1, AtMost(10.0)))
	if err != nil || n != 1 || !strings.Contains(statements[1], ` <= `) {
		t.Fatalf("set bound: %v %v", statements, err)
	}
	if _, err := r.Query().UpdateAll(t.Context(), db, r.Signed.Decrement(1, AtMost(int64(5)))); !errors.Is(err, fault.Invalid) {
		t.Fatalf("wrong bound direction: %v", err)
	}
}
func TestLockedAndPaginationValidationOrder(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r := records()
	db := testDB(t, testConnector{})
	if _, err := (Query[record]{}).Paginate(canceled, db, Page(0, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("pagination validation order: %v", err)
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := (LockedQuery[record]{}).All(canceled, tx); !errors.Is(err, context.Canceled) {
			t.Errorf("locked order: %v", err)
		}
		if _, err := r.Query().ForShare().Require(t.Context(), tx); !errors.Is(err, database.NotFound) || !strings.Contains(err.Error(), "locked model") {
			t.Errorf("locked missing: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []LockedQuery[record]{r.Query().ForUpdate().Where(r.ID.Eq(1)).OrderBy(r.Name.Asc()).Limit(1).Offset(0).WithTrashed().WithoutTrashed().NoWait(), r.Query().ForNoKeyUpdate().SkipLocked(), r.Query().ForKeyShare().Wait(), r.Query().ForShare()} {
		if _, err := CompileLocked(q); err != nil {
			t.Fatal(err)
		}
	}
}
func TestRestoreForceDeleteAndDeleteAll(t *testing.T) {
	r := timedRecords()
	q := r.Query().Where(r.Key().Eq(1))
	for _, kind := range []querycore.MutationKind{querycore.RestoreModel, querycore.ForceDeleteModel} {
		p, err := planWrite(q.state, kind, []Assignment[timed](nil), false)
		if err != nil {
			t.Fatal(err)
		}
		p, err = p.conventions(t.Context(), frozen(time.Now()))
		if err != nil {
			t.Fatal(err)
		}
		statement, err := p.compile()
		if err != nil {
			t.Fatal(err)
		}
		if kind == querycore.RestoreModel && !strings.Contains(statement.SQLText, `"deleted_at" IS NOT NULL`) {
			t.Fatal(statement.SQLText)
		}
		if kind == querycore.ForceDeleteModel && statement.SQLText != `DELETE FROM "timed" WHERE ("timed"."id" = $1) AND ("timed"."deleted_at" IS NULL) RETURNING "timed"."id", "timed"."created_at", "timed"."updated_at", "timed"."deleted_at"` {
			t.Fatal(statement.SQLText)
		}
	}
	db := testDB(t, testConnector{})
	if n, err := records().Query().DeleteAll(t.Context(), db); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestInsertManyHydratesAndValidatesEmpty(t *testing.T) {
	r := records()
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(1), "one", nil, "1"}, {int64(2), "two", nil, "2"}}})
	one, _ := decimal.Parse("1")
	two, _ := decimal.Parse("2")
	items, err := r.InsertMany(t.Context(), db, [][]Assignment[record]{{r.Name.Set("one"), r.Amount.Set(one)}, {r.Name.Set("two"), r.Amount.Set(two)}})
	if err != nil || len(items) != 2 || items[1].Name != "two" || len(statements) != 1 {
		t.Fatalf("batch: %v %v", items, err)
	}
	if _, err := r.InsertMany(t.Context(), db, nil); err != nil || len(statements) != 1 {
		t.Fatal("empty batch executed", err)
	}
}

// A synthetic overlapping model verifies the same ambiguity counter used by
// bindField, without constructing an out-of-bounds unsafe pointer.
func TestOffsetMatchRejectsZeroSizeNeighbors(t *testing.T) {
	typ := reflect.TypeFor[struct {
		A struct{}
		B struct{ Inner struct{} }
	}]()
	if offsetMatches(typ, 0, reflect.TypeFor[struct{}]()) < 2 {
		t.Fatal("ambiguous nested offset not detected")
	}
	var row record
	if err := hydrate(nil, nil, unsafe.Pointer(&row)); err == nil {
		t.Fatal("uninitialized hydration")
	}
}

func TestQueryTerminalsAndStreaming(t *testing.T) {
	r := records()
	db := testDB(t, testConnector{respond: func(s string) ([]string, [][]driver.Value) {
		if strings.HasPrefix(s, "SELECT COUNT") {
			return []string{"count"}, [][]driver.Value{{int64(1)}}
		}
		if strings.HasPrefix(s, "SELECT EXISTS") {
			return []string{"exists"}, [][]driver.Value{{true}}
		}
		return []string{"id", "display_name", "note", "amount"}, [][]driver.Value{{int64(1), "one", nil, "1"}}
	}})
	if item, err := r.Query().First(t.Context(), db); err != nil || !item.IsSet() {
		t.Fatal(item, err)
	}
	if item, err := r.Query().Require(t.Context(), db); err != nil || item.Name != "one" {
		t.Fatal(item, err)
	}
	if n, err := r.Query().Count(t.Context(), db); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if yes, err := r.Query().Exists(t.Context(), db); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if page, err := r.Query().Paginate(t.Context(), db, Page(1, 2)); err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if page, err := r.Query().SimplePaginate(t.Context(), db, Page(1, 2)); err != nil || page.HasMore || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	calls := 0
	if err := r.Query().Each(t.Context(), db, func(item record) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if item, err := r.Query().ForShare().First(t.Context(), tx); err != nil || !item.IsSet() {
			t.Error(item, err)
		}
		return r.Query().ForUpdate().Each(t.Context(), tx, func(record) error { return nil })
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAutocommitInsertUnknownRetainsCandidate(t *testing.T) {
	r := records()
	db := testDB(t, testConnector{columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(7), "stored", nil, "1"}}, closeErr: errors.New("close interrupted")})
	amount, _ := decimal.Parse("1")
	item, err := r.Insert(t.Context(), db, r.Name.Set("stored"), r.Amount.Set(amount))
	var reconciliation *WriteError[record]
	if item.ID != 0 || !errors.As(err, &reconciliation) || reconciliation.Candidate().ID != 7 {
		t.Fatalf("autocommit candidate: %v", err)
	}
}

func TestTimestampKindAndNullabilityValidation(t *testing.T) {
	type invalidTime struct {
		ID        int
		CreatedAt time.Time
		UpdatedAt time.Time
		DeletedAt time.Time
	}
	table := Define("bad_times", func(b Builder[invalidTime], m *invalidTime) Table[invalidTime, int] {
		ColumnOf(b, &m.CreatedAt, Codec(codec.Time().WithParameterType(codec.TypeText)))
		UpdatedAtOf(b, &m.UpdatedAt)
		TimeOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	})
	err := Check(table)
	if err == nil || !strings.Contains(err.Error(), "managed timestamps") || !strings.Contains(err.Error(), "soft deletion") {
		t.Fatalf("metadata validation: %v", err)
	}
}

func TestExplicitTimestampOverridesOtherConventionField(t *testing.T) {
	type row struct {
		ID        int
		CreatedAt time.Time
		MadeAt    time.Time
		UpdatedAt time.Time
	}
	table := Define("roles", func(b Builder[row], m *row) Table[row, int] {
		TimeOf(b, &m.CreatedAt)
		CreatedAtOf(b, &m.MadeAt)
		UpdatedAtOf(b, &m.UpdatedAt)
		return TableOf(b, &m.ID)
	})
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	if table.core.model.Timestamps.Created != "made_at" {
		t.Fatal("explicit role lost to a different convention field")
	}
}
