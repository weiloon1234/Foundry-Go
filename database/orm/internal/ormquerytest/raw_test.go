package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import "time"

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type rawRecord struct {
	ID   int64
	Name string
}

func rawRecords() orm.Table[rawRecord, int64] {
	return orm.Define("raw_records", func(b orm.Builder[rawRecord], m *rawRecord) orm.Table[rawRecord, int64] {
		orm.TextOf(b, &m.Name)
		return orm.TableOf(b, &m.ID)
	})
}

func TestRawModelColumnContract(t *testing.T) {
	r := rawRecords()
	for _, tc := range []struct {
		name      string
		columns   []string
		data      [][]driver.Value
		offending string
	}{
		{"reordered", []string{"name", "id"}, [][]driver.Value{{"Ada", int64(9)}}, ""},
		{"missing", []string{"id"}, [][]driver.Value{{int64(9)}}, "name"},
		{"duplicate", []string{"id", "id", "name"}, nil, "id"},
		{"extra", []string{"id", "name", "extra"}, nil, "extra"},
		{"zero_missing", []string{"name"}, nil, "id"},
		{"zero_valid", []string{"id", "name"}, nil, ""},
		{"decode", []string{"id", "name"}, [][]driver.Value{{int64(9), "Ada"}, {"invalid integer", "private"}}, "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed := false
			db := testDB(t, testConnector{columns: tc.columns, values: tc.data, onClose: func() { closed = true }})
			got, err := orm.Raw(t.Context(), db, r, "raw statement", "argument-canary")
			if !closed {
				t.Fatal("rows not closed")
			}
			if tc.offending != "" {
				if err == nil || got != nil {
					t.Fatal("published invalid result", got, err)
				}
				if tc.offending != "decode" && (!errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), tc.offending) || strings.Contains(err.Error(), "argument-canary") || strings.Contains(err.Error(), "raw statement")) {
					t.Fatal("column diagnostic", err)
				}
			} else if err != nil || (len(tc.data) > 0 && (len(got) != 1 || got[0] != (rawRecord{9, "Ada"}))) {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRawModelExecutorsAndCancellation(t *testing.T) {
	r := rawRecords()
	calls, closed := 0, 0
	db := testDB(t, testConnector{onClose: func() { closed++ }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		calls++
		if sql != "UPDATE raw_records SET name=$1 RETURNING id,name" || len(args) != 1 || args[0].Value != "Ada" {
			t.Fatal("raw SQL/args changed", sql, args)
		}
		return []string{"id", "name"}, [][]driver.Value{{int64(9), "Ada"}}, nil
	}})
	run := func(ex database.Executor) error {
		got, err := orm.Raw(t.Context(), ex, r, "UPDATE raw_records SET name=$1 RETURNING id,name", "Ada")
		if err == nil && len(got) != 1 {
			t.Fatal(got)
		}
		return err
	}
	if err := run(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error { return run(tx) }); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || closed != 2 {
		t.Fatal(calls, closed)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := orm.Raw(ctx, db, r, "unused"); !errors.Is(err, context.Canceled) || got != nil || calls != 2 {
		t.Fatal(got, err, calls)
	}
	var invalid orm.Table[rawRecord, int64]
	if got, err := orm.Raw(t.Context(), db, invalid, "unused"); err == nil || got != nil || calls != 2 {
		t.Fatal("invalid declaration sent SQL", err)
	}
}

func TestRawModelCloseFailure(t *testing.T) {
	sentinel := errors.New("close failure")
	db := testDB(t, testConnector{columns: []string{"id", "name"}, closeErr: sentinel})
	got, err := orm.Raw(t.Context(), db, rawRecords(), "raw")
	if got != nil || !errors.Is(err, sentinel) {
		t.Fatal(got, err)
	}
}

func TestRawEncryptedModel(t *testing.T) {
	r := secureRecords()
	keys := sensitiveKeys(t)
	id, err := model.NewIDAt[secureRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := encrypted.Binding("secure_records", "secret", id.String())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypted.NewText("private").Seal(t.Context(), keys, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encrypted.TextCodec().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{columns: []string{"secret", "backup", "id"}, values: [][]driver.Value{{raw, nil, id.String()}}}, database.WithEncryption(keys))
	got, err := orm.Raw(t.Context(), db, r.Table, "SELECT secret,backup,id FROM secure_records")
	if err != nil || len(got) != 1 || got[0].Secret.Reveal() != "private" || got[0].ID != id {
		t.Fatal("encrypted raw hydration", err)
	}
}

type cancelRawExecutor struct {
	database.Executor
	cancel       context.CancelFunc
	queryContext context.Context
}

func (e cancelRawExecutor) Query(_ context.Context, sql string, args ...any) (*database.Rows, error) {
	rows, err := e.Executor.Query(e.queryContext, sql, args...)
	e.cancel()
	return rows, err
}
func TestRawCancellationClosesAcquiredRows(t *testing.T) {
	closed := false
	db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "private"}}, onClose: func() { closed = true }})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got, err := orm.Raw(ctx, cancelRawExecutor{db, cancel, t.Context()}, rawRecords(), "raw")
	if !errors.Is(err, context.Canceled) || got != nil || !closed {
		t.Fatal(got, err, closed)
	}
}

func TestRawQuotedAliasesAreCaseSensitive(t *testing.T) {
	for _, alias := range []string{"Id", "id"} {
		t.Run(alias, func(t *testing.T) {
			closed := false
			statement := `SELECT id AS "` + alias + `", name FROM raw_records`
			db := testDB(t, testConnector{onClose: func() { closed = true }, respond: func(sql string) ([]string, [][]driver.Value) {
				if sql != statement {
					t.Fatal("raw SQL changed", sql)
				}
				return []string{alias, "name"}, [][]driver.Value{{int64(9), "Ada"}}
			}})
			got, err := orm.Raw(t.Context(), db, rawRecords(), statement)
			if !closed {
				t.Fatal("rows not closed")
			}
			if alias == "Id" {
				if got != nil || !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), `unexpected column "Id"`) || !strings.Contains(err.Error(), `missing column "id"`) {
					t.Fatal(got, err)
				}
			} else if err != nil || len(got) != 1 || got[0] != (rawRecord{9, "Ada"}) {
				t.Fatal(got, err)
			}
		})
	}
}

// driver.Rows.Columns cannot return an error. Close the driver-backed stream
// before handing it to Raw to exercise the fallible database.Rows.Columns path.
type closedRawExecutor struct{ database.Executor }

func (e closedRawExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	rows, err := e.Executor.Query(ctx, sql, args...)
	if err == nil {
		_ = rows.Close()
	}
	return rows, err
}
func TestRawColumnsFailureClosesAndPublishesNothing(t *testing.T) {
	sentinel := errors.New("injected driver close failure")
	closes := 0
	db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(9), "Ada"}}, closeErr: sentinel, onClose: func() { closes++ }})
	got, err := orm.Raw(t.Context(), closedRawExecutor{db}, rawRecords(), "SELECT id,name FROM raw_records")
	if got != nil || !errors.Is(err, database.Closed) || !errors.Is(err, sentinel) || closes != 1 {
		t.Fatal(got, err, closes)
	}
}
