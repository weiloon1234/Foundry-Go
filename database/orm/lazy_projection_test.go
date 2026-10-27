package orm_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/internal/enuminit"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type lazyDriver struct{ queries int }

func (d *lazyDriver) Driver() driver.Driver                        { return d }
func (d *lazyDriver) Open(string) (driver.Conn, error)             { return d, nil }
func (d *lazyDriver) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (*lazyDriver) Prepare(string) (driver.Stmt, error)            { return nil, errors.New("unexpected prepare") }
func (*lazyDriver) Close() error                                   { return nil }
func (*lazyDriver) Begin() (driver.Tx, error)                      { return nil, errors.New("unexpected transaction") }
func (*lazyDriver) Ping(context.Context) error                     { return nil }
func (d *lazyDriver) QueryContext(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
	d.queries++
	return &lazyRows{scalar: strings.Contains(sql, `AS "value"`)}, nil
}

type lazyRows struct{ scalar, done bool }

func (r *lazyRows) Columns() []string {
	if r.scalar {
		return []string{"value"}
	}
	return []string{"state", "nullable"}
}
func (*lazyRows) Close() error { return nil }
func (r *lazyRows) Next(dst []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dst[0] = "active"
	if len(dst) > 1 {
		dst[1] = nil
	}
	return nil
}
func lazyDB(t *testing.T) (*database.DB, *lazyDriver) {
	t.Helper()
	d := &lazyDriver{}
	db, err := database.Open(t.Context(), database.Adapter{Connector: d}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db, d
}
func TestPackageLevelProjectionWaitsForEnumInitialization(t *testing.T) {
	db, d := lazyDB(t)
	got, err := rows.Read(enuminit.Projection.Where(enuminit.State.Eq(enuminit.Active))).All(t.Context(), db)
	if err != nil || len(got) != 1 || got[0].State != enuminit.Active || !got[0].Nullable.IsNull() {
		t.Fatalf("projection: %+v %v", got, err)
	}
	values, err := rows.Read(enuminit.Values).All(t.Context(), db)
	if err != nil || len(values) != 1 || values[0] != enuminit.Active {
		t.Fatal(values, err)
	}
	if d.queries != 2 {
		t.Fatal(d.queries)
	}
}

var badDescriptorCalls int

type badStatus string

func (badStatus) EnumDescriptor() enum.Descriptor[badStatus] {
	badDescriptorCalls++
	return enum.Descriptor[badStatus]{}
}
func TestLazyProjectionErrorsBeforeSQL(t *testing.T) {
	type model struct {
		ID    int
		State badStatus
	}
	type table struct {
		orm.Table[model, int]
		State orm.Enum[model, badStatus]
	}
	badDescriptorCalls = 0
	tbl := orm.Define("bad_lazy", func(b orm.Builder[model], m *model) table {
		return table{orm.TableOf(b, &m.ID), orm.EnumOf(b, &m.State)}
	})
	alias := rows.As[struct{}](tbl.Query(), "b")
	_ = expr.At(rows.Scope(alias), tbl.State)
	q := rows.SelectValue(tbl.Query(), tbl.Key()) // Even an unselected invalid enum must be checked.
	if badDescriptorCalls != 0 {
		t.Fatal("descriptor resolved during construction")
	}
	db, d := lazyDB(t)
	if _, err := rows.Read(q).All(t.Context(), db); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if badDescriptorCalls != 1 || d.queries != 0 {
		t.Fatal("late validation or SQL", badDescriptorCalls, d.queries)
	}
	type dto struct{ State badStatus }
	type fields struct {
		rows.Row[dto]
		State rows.Field[dto, badStatus]
	}
	schema := rows.Define(func(b rows.Builder[dto], r *dto) fields { return fields{b.Row(), rows.EnumOf(b, &r.State)} })
	// A row-only invalid descriptor cannot be masked by valid source metadata.
	// Use the same invalid enum type through an explicit string codec, leaving the
	// table valid; only the row constructor supplies descriptor validation.
	type textModel struct {
		ID    int
		State badStatus
	}
	type textTable struct {
		orm.Table[textModel, int]
		State orm.Text[textModel, badStatus]
	}
	text := orm.Define("text_lazy", func(b orm.Builder[textModel], m *textModel) textTable {
		return textTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.State)}
	})
	a := rows.As[struct{}](text.Query(), "t")
	before := badDescriptorCalls
	selected := rows.Select(a, schema, rows.Map(schema.State, expr.At(rows.Scope(a), text.State)))
	if badDescriptorCalls != before {
		t.Fatal("row checked during Select")
	}
	if _, err := rows.Read(selected).All(t.Context(), db); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if badDescriptorCalls != before+1 || d.queries != 0 {
		t.Fatal("row check missing", badDescriptorCalls, d.queries)
	}
}
