package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

func TestExplainResultShapesAndCursor(t *testing.T) {
	m := declareMembers()
	schema := declareRows()
	for _, kind := range []string{"projection", "scalar", "set", "cursor", "value_cursor", "locked", "transaction", "transaction_value"} {
		t.Run(kind, func(t *testing.T) {
			document := `[{"Plan":{"Node Type":"Seq Scan","Startup Cost":0,"Total Cost":2,"Plan Rows":1,"Plan Width":8}}]`
			if kind == "set" || kind == "value_cursor" {
				document = `[{"Plan":{"Node Type":"Seq Scan","Startup Cost":0,"Total Cost":2,"Plan Rows":1,"Plan Width":8,"Actual Rows":1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":0.1},"Execution Time":0.2}]`
			}
			state := &driverState{columns: []string{"QUERY PLAN"}, rows: [][]driver.Value{{document}}}
			db, openErr := database.Open(t.Context(), database.Adapter{Connector: recordConnector{state}}, database.DefaultPoolConfig())
			if openErr != nil {
				t.Fatal(openErr)
			}
			t.Cleanup(func() { db.Close(context.Background()) })
			ctx := t.Context()
			var plan orm.Plan
			var err error
			projection := Select(From(m.Query()), schema, Map(schema.ID, expr.Value(m.ID)), Map(schema.Name, expr.Value(m.Name)))
			switch kind {
			case "projection":
				plan, err = Explain(ctx, db, Read(projection))
			case "scalar":
				plan, err = Explain(ctx, db, ReadScalar(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value())))
			case "set":
				plan, err = ExplainAnalyze(ctx, db, ReadSet(Union(ModelRecord(m.Query()), ModelRecord(m.Query()))))
			case "cursor":
				c := CursorFor(ModelRecord(m.Query()))
				c = c.UniqueBy(expr.Group(expr.At(c.Scope(), m.ID)))
				plan, err = ExplainCursor(ctx, db, c)
			case "value_cursor":
				c := ValueCursorFor(Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value())))
				plan, err = ExplainAnalyzeValueCursor(ctx, db, c.UniqueBy(expr.Group(c.Value())))
			case "locked":
				err = db.Transaction(ctx, func(tx *database.Tx) error {
					plan, err = ExplainTransaction(ctx, tx, ForUpdate(Read(projection)))
					return err
				})
			case "transaction":
				err = db.Transaction(ctx, func(tx *database.Tx) error {
					plan, err = ExplainTransaction(ctx, tx, LockedRecord(m.Query().ForUpdate()))
					return err
				})
			case "transaction_value":
				err = db.Transaction(ctx, func(tx *database.Tx) error {
					source := AsTransaction[left](LockedRecord(m.Query().ForUpdate()), "locked")
					plan, err = ExplainTransactionValue(ctx, tx, SelectTransactionValue(source, expr.Row(expr.At(TransactionScope(source), m.ID)).Value()))
					return err
				})
			}
			if err != nil || plan.NodeCount() != 1 || !strings.HasPrefix(state.sql, "EXPLAIN") {
				t.Fatal(err, state.sql)
			}
			if strings.Contains(kind, "cursor") && strings.Contains(state.sql, "LIMIT") {
				t.Fatal("explain invented a cursor page", state.sql)
			}
		})
	}
	state := &driverState{}
	db := openTest(t, state)
	if _, err := ExplainTransaction(context.Background(), nil, ForUpdate(Read(FromModel(m.Query())))); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	c := CursorFor(ModelRecord(m.Query()))
	if _, err := ExplainCursor(t.Context(), db, c); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestExplainCodecFailuresAreRedacted(t *testing.T) {
	type model struct {
		ID   int64
		Name string
	}
	type table struct {
		orm.Table[model, int64]
		Name orm.Text[model, string]
	}
	cause := &privateExplainError{value: "private-row-binding"}
	c := codec.New(func(string) (driver.Value, error) { return nil, cause }, func(v any) (string, error) { return v.(string), nil })
	f := orm.Define("private_rows", func(b orm.Builder[model], m *model) table {
		return table{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Codec(c))}
	})
	state := &driverState{}
	db, err := database.Open(t.Context(), database.Adapter{Connector: recordConnector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	_, err = Explain(t.Context(), db, ReadScalar(SelectScalar(From(f.Query().Where(f.Name.Eq(cause.value))), expr.Row(expr.Value(f.Name)).Value())))
	var typed *privateExplainError
	if !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause || state.sql != "" {
		t.Fatal("lost codec error identity", err)
	}
	for _, s := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(s, cause.value) {
			t.Fatal("codec error disclosed binding")
		}
	}
}

type privateExplainError struct{ value string }

func (e *privateExplainError) Error() string { return e.value }
func TestExplainRejectsInvalidSourceBeforeDeclarationCallbacks(t *testing.T) {
	m := declareMembers()
	q := ReadScalar(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value()))
	called := 0
	q.core.Source.Checks = []func() error{func() error { called++; return nil }}
	q.core.Transaction = true
	state := &driverState{}
	if _, e := Explain(t.Context(), openTest(t, state), q); !errors.Is(e, fault.Invalid) || called != 0 || state.sql != "" {
		t.Fatal("invalid plan reached declaration callback or SQL", e, called, state.sql)
	}
}
