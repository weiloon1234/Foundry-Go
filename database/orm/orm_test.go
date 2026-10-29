package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type record struct {
	ID      int64  `foundry:"default=database"`
	Name    string `foundry:"column=display_name"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal
	Payload []byte `foundry:"-"`
	Related relation.One[record]
}
type recordTable struct {
	Table[record, int64]
	ID     Key[record, int64]
	Name   Text[record, string]
	Note   NullableText[record, string]
	Amount Number[record, decimal.Decimal]
}

func records() recordTable {
	return Define("records", func(b Builder[record], r *record) recordTable {
		t := TableOf(b, &r.ID)
		return recordTable{Table: t, ID: t.Key(), Name: TextOf(b, &r.Name), Note: NullableTextOf(b, &r.Note), Amount: NumberOf(b, &r.Amount)}
	})
}

func TestBindingAndNaturalKey(t *testing.T) {
	r := records()
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	if r.core.model.Columns[1].Name != "display_name" || !r.core.model.Columns[0].DatabaseDefault {
		t.Fatal("field tags were not applied")
	}
	type CountryCode string
	type country struct {
		Code CountryCode
		Name string
	}
	type countries struct {
		Table[country, CountryCode]
		Name Text[country, string]
	}
	c := Define("countries", func(b Builder[country], m *country) countries {
		return countries{Table: TableOf(b, &m.Code), Name: TextOf(b, &m.Name)}
	})
	if err := Check(c); err != nil {
		t.Fatal(err)
	}
	s, err := Compile(c.Query().Where(KeyEq(c.Key(), CountryCode("MY"))))
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{"MY"}) {
		t.Fatalf("natural key: %s %v", s.SQL(), err)
	}
}

func TestDeclarationErrors(t *testing.T) {
	type sample struct {
		ID    int64
		Name  string
		Extra map[string]string
	}
	cases := map[string]func(Builder[sample], *sample) Table[sample, int64]{
		"offset": func(b Builder[sample], s *sample) Table[sample, int64] {
			foreign := new(string)
			TextOf(b, foreign)
			return TableOf(b, &s.ID)
		},
		"undeclared": func(b Builder[sample], s *sample) Table[sample, int64] { return TableOf(b, &s.ID) },
		"unsupported": func(b Builder[sample], s *sample) Table[sample, int64] {
			ColumnOf(b, &s.Extra)
			TextOf(b, &s.Name)
			return TableOf(b, &s.ID)
		},
		"duplicate binding": func(b Builder[sample], s *sample) Table[sample, int64] {
			TextOf(b, &s.Name)
			TextOf(b, &s.Name)
			return TableOf(b, &s.ID)
		},
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			table := Define("samples", declare)
			err := Check(table)
			if err == nil {
				t.Fatal("invalid declaration accepted")
			}
			if _, err = Compile(table.Query()); err == nil {
				t.Fatal("invalid table read accepted")
			}
			if _, err = Insert(context.Background(), nil, table); err == nil {
				t.Fatal("invalid table write accepted")
			}
		})
	}
	type badTag struct {
		ID   int
		Name string `foundry:"column=not valid,default=oops,typo=yes"`
	}
	bad := Define("bad", func(b Builder[badTag], s *badTag) Table[badTag, int] { TextOf(b, &s.Name); return TableOf(b, &s.ID) })
	err := Check(bad)
	for _, want := range []string{"column tag", "default=database", "typo"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
	type duplicate struct {
		ID     int
		First  string `foundry:"column=same"`
		Second string `foundry:"column=same"`
	}
	dup := Define("duplicates", func(b Builder[duplicate], s *duplicate) Table[duplicate, int] {
		TextOf(b, &s.First)
		TextOf(b, &s.Second)
		return TableOf(b, &s.ID)
	})
	if err := Check(dup); err == nil {
		t.Fatal("duplicate column accepted")
	}
	type nested struct {
		ID    int
		Inner struct{ Name string }
	}
	n := Define("nested", func(b Builder[nested], s *nested) Table[nested, int] {
		TextOf(b, &s.Inner.Name)
		return TableOf(b, &s.ID)
	})
	if err := Check(n); err == nil {
		t.Fatal("nested field accepted")
	}
	if joined := Check(bad, dup); joined == nil || !strings.Contains(joined.Error(), "bad.Name") || !strings.Contains(joined.Error(), "duplicates") {
		t.Fatalf("errors not collected: %v", joined)
	}
}

func TestSealedAndOwnedDeclarations(t *testing.T) {
	var builder Builder[record]
	var template *record
	r := Define("records", func(b Builder[record], m *record) recordTable {
		builder = b
		template = m
		x := TableOf(b, &m.ID)
		return recordTable{Table: x, ID: x.Key(), Name: TextOf(b, &m.Name), Note: NullableTextOf(b, &m.Note), Amount: NumberOf(b, &m.Amount)}
	})
	late := TextOf(builder, &template.Name)
	if _, err := Compile(r.Query().Where(TextEq(late, "late"))); err == nil {
		t.Fatal("sealed builder allowed a usable column")
	}
	if err := Check(r); err != nil {
		t.Fatalf("late declaration mutated original: %v", err)
	}
	other := records()
	if _, err := Compile(r.Query().Where(TextEq(other.Name, "different declaration"))); err == nil {
		t.Fatal("cross-declaration predicate accepted")
	}
	base := r.Query().Where(KeyEq(r.ID, 1))
	_, _ = Compile(base.Where(TextEq(r.Name, "branch")))
	s, err := Compile(base)
	if err != nil || strings.Contains(s.SQL(), "display_name\" =") {
		t.Fatalf("query mutated: %s %v", s.SQL(), err)
	}
}

type memoryRows struct {
	values     [][]any
	next       int
	current    []any
	failure    error
	closeError error
	closed     bool
}

func (r *memoryRows) Next() bool {
	if r.closed || r.next == len(r.values) {
		return false
	}
	r.current = r.values[r.next]
	r.next++
	return true
}
func (r *memoryRows) Scan(dest ...any) error {
	if len(dest) != len(r.current) {
		return errors.New("column mismatch")
	}
	for i, d := range dest {
		if err := d.(sql.Scanner).Scan(r.current[i]); err != nil {
			return err
		}
	}
	return nil
}
func (r *memoryRows) Err() error   { return r.failure }
func (r *memoryRows) Close() error { r.closed = true; return r.closeError }
func memoryAll[M any](ctx context.Context, table *tableCore, rows rowSource) ([]M, error) {
	items := []M{}
	err := consume(ctx, rows, func(row database.Row) error {
		var m M
		if err := hydrate(table, row, unsafe.Pointer(&m)); err != nil {
			return err
		}
		items = append(items, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}
func TestOffsetHydrationDiscardsPartialModels(t *testing.T) {
	table := records()
	rows := &memoryRows{values: [][]any{{int64(7), "first", nil, "12.50"}, {int64(8), []byte("second"), "", "0"}}}
	items, err := memoryAll[record](t.Context(), table.core, rows)
	if err != nil || len(items) != 2 || items[0].ID != 7 || items[1].Name != "second" || !rows.closed {
		t.Fatalf("hydration: %+v %v", items, err)
	}
	if _, ok := items[0].Note.Get(); ok {
		t.Fatal("NULL became a value")
	}
	if n, ok := items[1].Note.Get(); !ok || n != "" {
		t.Fatal("empty string became NULL")
	}
	for _, source := range []*memoryRows{
		{values: [][]any{{int64(1), "first", nil, "1"}, {int64(2), "partial", nil, float64(2)}}},
		{values: [][]any{{int64(1), "first", nil, "1"}}, failure: errors.New("iterate")},
		{values: [][]any{{int64(1), "first", nil, "1"}}, closeError: errors.New("close")},
	} {
		items, err := memoryAll[record](t.Context(), table.core, source)
		if err == nil || items != nil || !source.closed {
			t.Fatal("partial hydration escaped")
		}
	}
}
func TestAssignmentSemanticsAndValidation(t *testing.T) {
	r := records()
	amount, err := decimal.Parse("12.50")
	if err != nil {
		t.Fatal(err)
	}
	p, err := planWrite(r.Query().state, querycore.InsertModel, []Assignment[record]{TextSet(r.Name, ""), NullableTextSetNull(r.Note), NumberSet(r.Amount, amount)}, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.compile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.SQLText, `("id"`) || !reflect.DeepEqual(s.Bindings, []any{"", nil, "12.5"}) {
		t.Fatalf("omission/zero/null: %s %#v", s.SQLText, s.Bindings)
	}
	empty, _ := planWrite(r.Query().state, querycore.InsertModel, []Assignment[record]{NullableTextSetNull(r.Note)}, false)
	if _, err := empty.compile(); err == nil {
		t.Fatal("required fields not checked")
	}
	for name, as := range map[string][]Assignment[record]{"duplicate": {TextSet(r.Name, "a"), TextSet(r.Name, "b")}, "key": {KeySet(r.ID, 9)}, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			p, err := planWrite(r.Query().Where(KeyEq(r.ID, 1)).state, querycore.UpdateModel, as, false)
			if err == nil {
				_, err = p.compile()
			}
			if err == nil {
				t.Fatal("invalid update accepted")
			}
		})
	}
	for _, q := range []Query[record]{r.Query().Limit(1), r.Query().Offset(1), r.Query().OrderBy(KeyAsc(r.ID))} {
		if _, err := planWrite(q.state, querycore.UpdateModel, []Assignment[record]{TextSet(r.Name, "x")}, true); err == nil {
			t.Fatal("write discarded a read clause")
		}
	}
	type binary struct {
		ID   int
		Data []byte
	}
	type binaryTable struct {
		Table[binary, int]
		Data Column[binary, []byte]
	}
	b := Define("binary", func(b Builder[binary], m *binary) binaryTable {
		return binaryTable{TableOf(b, &m.ID), BytesOf(b, &m.Data)}
	})
	data := []byte{1, 2}
	a := ColumnSet(b.Data, data)
	data[0] = 9
	bound, err := a.core.Bind()
	if err != nil || bound.([]byte)[0] != 1 {
		t.Fatal("assignment borrowed caller bytes")
	}
}

type timed struct {
	ID        int
	CreatedAt temporal.DateTime
	UpdatedAt temporal.DateTime
	DeletedAt value.Nullable[temporal.DateTime]
}
type timedTable struct {
	Table[timed, int]
	CreatedAt CreatedAt[timed, temporal.DateTime]
	UpdatedAt UpdatedAt[timed, temporal.DateTime]
	DeletedAt SoftDelete[timed, temporal.DateTime]
}

func timedRecords(opts ...Option) timedTable {
	return Define("timed", func(b Builder[timed], m *timed) timedTable {
		return timedTable{TableOf(b, &m.ID), CreatedAtOf(b, &m.CreatedAt), UpdatedAtOf(b, &m.UpdatedAt), SoftDeleteOf(b, &m.DeletedAt)}
	}, opts...)
}

type frozen time.Time

func (f frozen) Now() time.Time { return time.Time(f) }
func TestTimestampsAndSoftDeletion(t *testing.T) {
	table := timedRecords()
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 1, 2, 3, 123456789, time.UTC)
	explicit, err := temporal.NewDateTime(now.Add(-time.Hour).Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	p, err := planWrite(table.Query().state, querycore.InsertModel, []Assignment[timed]{KeySet(table.Key(), 1), TimeSet(table.CreatedAt, explicit), TimeSet(table.UpdatedAt, explicit)}, false)
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.conventions(t.Context(), frozen(now))
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.compile()
	if err != nil {
		t.Fatal(err)
	}
	if s.Bindings[1] != explicit.UTC() || s.Bindings[2] != now.Truncate(time.Microsecond) {
		t.Fatalf("timestamp presence: %#v", s.Bindings)
	}
	read, err := Compile(table.Query())
	if err != nil || !strings.Contains(read.SQL(), `"deleted_at" IS NULL`) {
		t.Fatalf("soft scope: %s %v", read.SQL(), err)
	}
	p, err = planWrite(table.Query().Where(KeyEq(table.Key(), 1)).state, querycore.DeleteModel, []Assignment[timed](nil), false)
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.conventions(t.Context(), frozen(now))
	if err != nil {
		t.Fatal(err)
	}
	s, err = p.compile()
	if err != nil || !strings.HasPrefix(s.SQLText, "UPDATE") || !strings.Contains(s.SQLText, `"deleted_at" IS NULL`) {
		t.Fatalf("soft delete: %s %v", s.SQLText, err)
	}
	if unmanagedTimed(NoTimestamps()).core.model.Timestamps != nil {
		t.Fatal("NoTimestamps ignored")
	}
}
func TestUUIDKeyDefaults(t *testing.T) {
	type uuidRecord struct{ ID model.ID[uuidRecord] }
	table := Define("uuids", func(b Builder[uuidRecord], m *uuidRecord) Table[uuidRecord, model.ID[uuidRecord]] {
		return TableOf(b, &m.ID)
	})
	p, err := planWrite(table.Query().state, querycore.InsertModel, []Assignment[uuidRecord](nil), false)
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.conventions(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.compile()
	if err != nil || len(s.Bindings) != 1 {
		t.Fatalf("UUID omission: %v", err)
	}
	id, err := model.ParseID[uuidRecord](s.Bindings[0].(string))
	if err != nil || id.IsZero() {
		t.Fatal("UUID not generated")
	}
}
func TestPublicMethodBudgets(t *testing.T) {
	types := []reflect.Type{reflect.TypeFor[Table[record, int64]](), reflect.TypeFor[Builder[record]](), reflect.TypeFor[Query[record]](), reflect.TypeFor[LockedQuery[record]](), reflect.TypeFor[Column[record, int]](), reflect.TypeFor[Key[record, int]](), reflect.TypeFor[Number[record, int]](), reflect.TypeFor[Text[record, string]](), reflect.TypeFor[NullableText[record, string]](), reflect.TypeFor[NullableNumber[record, int]](), reflect.TypeFor[Time[record, time.Time]](), reflect.TypeFor[NullableTime[record, time.Time]](), reflect.TypeFor[Predicate[record]](), reflect.TypeFor[Order[record]](), reflect.TypeFor[Assignment[record]](), reflect.TypeFor[ColumnOption[int]](), reflect.TypeFor[PageResult[record]](), reflect.TypeFor[*WriteError[record]]()}
	for _, typ := range types {
		if typ.NumMethod() > 25 {
			t.Errorf("%s has %d methods", typ, typ.NumMethod())
		}
	}
}

// The connector exercises real database.Rows and transaction ownership without a server.
type testConnector struct {
	affected     *int64
	statements   *[]string
	bindings     *[][]any
	onClose      func()
	onBegin      func()
	values       [][]driver.Value
	columns      []string
	execError    func(string) error
	commitErr    error
	closeErr     error
	respondBound func(string, []driver.NamedValue) ([]string, [][]driver.Value, error)
	respond      func(string) ([]string, [][]driver.Value)
}

func (c testConnector) Connect(context.Context) (driver.Conn, error) { return testConnection{c}, nil }
func (c testConnector) Driver() driver.Driver                        { return c }
func (c testConnector) Open(string) (driver.Conn, error)             { return testConnection{c}, nil }

type testConnection struct{ testConnector }

func (c testConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c testConnection) Close() error { return nil }
func (c testConnection) Begin() (driver.Tx, error) {
	if c.onBegin != nil {
		c.onBegin()
	}
	return testTransaction{c.commitErr}, nil
}
func (c testConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c testConnection) Ping(context.Context) error { return nil }
func (c testConnection) ExecContext(_ context.Context, s string, _ []driver.NamedValue) (driver.Result, error) {
	if c.statements != nil {
		*c.statements = append(*c.statements, s)
	}
	if c.execError != nil {
		if err := c.execError(s); err != nil {
			return nil, err
		}
	}
	if c.affected != nil {
		return driver.RowsAffected(*c.affected), nil
	}
	return driver.RowsAffected(1), nil
}
func (c testConnection) QueryContext(_ context.Context, s string, args []driver.NamedValue) (driver.Rows, error) {
	if c.statements != nil {
		*c.statements = append(*c.statements, s)
	}
	if c.bindings != nil {
		values := make([]any, len(args))
		for i, a := range args {
			values[i] = a.Value
		}
		*c.bindings = append(*c.bindings, values)
	}
	if c.respondBound != nil {
		columns, values, err := c.respondBound(s, args)
		if err != nil {
			return nil, err
		}
		return &testDriverRows{columns: columns, values: values, closeErr: c.closeErr, onClose: c.onClose}, nil
	}
	if c.respond != nil {
		columns, values := c.respond(s)
		return &testDriverRows{columns: columns, values: values, closeErr: c.closeErr, onClose: c.onClose}, nil
	}
	return &testDriverRows{columns: c.columns, values: c.values, closeErr: c.closeErr, onClose: c.onClose}, nil
}

type testTransaction struct{ err error }

func (tx testTransaction) Commit() error   { return tx.err }
func (tx testTransaction) Rollback() error { return nil }

type testDriverRows struct {
	columns  []string
	values   [][]driver.Value
	index    int
	onClose  func()
	closeErr error
}

func (r *testDriverRows) Columns() []string { return r.columns }
func (r *testDriverRows) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return r.closeErr
}
func (r *testDriverRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
func testDB(t *testing.T, c testConnector, opts ...database.Option) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: c}, database.DefaultPoolConfig(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}
func TestReadAndWriteTerminals(t *testing.T) {
	r := records()
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(7), "stored", nil, "12.5"}}})
	items, err := All(t.Context(), db, r.Query())
	if err != nil || len(items) != 1 || items[0].Name != "stored" {
		t.Fatalf("All: %v", err)
	}
	found, err := Find(t.Context(), db, r.Table, 7)
	if err != nil || !found.IsSet() {
		t.Fatalf("Find: %v", err)
	}
	updated, err := Update(t.Context(), db, r.Table.Query(), r.Table.Key(), 7, TextSet(r.Name, "stored"), NullableTextSetNull(r.Note))
	if err != nil || updated.ID != 7 {
		t.Fatalf("Update: %v", err)
	}
	n, err := UpdateAll(t.Context(), db, r.Query(), NullableTextSetNull(r.Note))
	if err != nil || n != 1 {
		t.Fatalf("UpdateAll: %v", err)
	}
	removed, err := Delete(t.Context(), db, r.Table.Query(), r.Table.Key(), 7)
	if err != nil || removed.ID != 7 {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.Contains(statements[1], `ORDER BY "records"."id" ASC LIMIT $2`) {
		t.Fatalf("Find order: %s", statements[1])
	}
	if _, err := AllLocked(t.Context(), nil, Lock(r.Query())); err == nil {
		t.Fatal("nil transaction accepted")
	}
}
func TestUnknownCommitRetainsTypedCandidate(t *testing.T) {
	r := records()
	db := testDB(t, testConnector{columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(7), "stored", nil, "12.5"}}, commitErr: errors.New("commit interrupted")})
	item, err := Update(t.Context(), db, r.Table.Query(), r.Table.Key(), 7, TextSet(r.Name, "stored"))
	var reconciliation *WriteError[record]
	if item.ID != 0 || !errors.As(err, &reconciliation) || reconciliation.Candidate().ID != 7 {
		t.Fatalf("candidate lost: %v", err)
	}
}
func TestUnsupportedCustomCodecCanBeSupplied(t *testing.T) {
	type custom struct{ Value string }
	type holder struct {
		ID     int
		Custom custom
	}
	table := Define("holders", func(b Builder[holder], m *holder) Table[holder, int] {
		ColumnOf(b, &m.Custom, Codec(codec.New(func(v custom) (driver.Value, error) { return v.Value, nil }, func(v any) (custom, error) {
			s, ok := v.(string)
			if !ok {
				return custom{}, errors.New("text required")
			}
			return custom{s}, nil
		})))
		return TableOf(b, &m.ID)
	})
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedTagsAndIncompleteTimestampConvention(t *testing.T) {
	type emptyTag struct {
		ID int `foundry:""`
	}
	empty := Define("empty", func(b Builder[emptyTag], m *emptyTag) Table[emptyTag, int] { return TableOf(b, &m.ID) })
	if err := Check(empty); err == nil {
		t.Fatal("empty foundry tag accepted")
	}
	// Build the malformed tag dynamically so go vet can still check static declarations.
	typ := reflect.StructOf([]reflect.StructField{{Name: "Name", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(`foundry:"column=broken`)}})
	core := &tableCore{model: querycore.ModelDefinition{Table: "bad"}}
	d := &declaration{core: core}
	c := &columnCore{field: typ.Field(0)}
	parseTag(d, c)
	if len(d.errors) != 1 || !strings.Contains(d.errors[0].Error(), "malformed quoted") {
		t.Fatalf("malformed tag disappeared: %v", d.errors)
	}
	type half struct {
		ID        int
		CreatedAt time.Time
	}
	table := Define("half", func(b Builder[half], m *half) Table[half, int] {
		TimeOf(b, &m.CreatedAt)
		return TableOf(b, &m.ID)
	})
	if err := Check(table); err != nil || table.core.model.Timestamps != nil {
		t.Fatalf("lone timestamp differs from generator convention: %v", err)
	}
	var missing *recordTable
	if err := Check(missing); err == nil {
		t.Fatal("nil table accepted")
	}
}

func unmanagedTimed(opts ...Option) Table[timed, int] {
	return Define("timed", func(b Builder[timed], m *timed) Table[timed, int] {
		TimeOf(b, &m.CreatedAt)
		TimeOf(b, &m.UpdatedAt)
		NullableTimeOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	}, append(opts, NoTimestamps(), NoSoftDeletes())...)
}
