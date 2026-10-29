package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func autocommitDecoderModel(decode func(database.Row) (mutationRecord, error)) mutationTable {
	return orm.Define("records", func(b orm.Builder[mutationRecord], m *mutationRecord) mutationTable {
		c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(raw any) (string, error) {
			v, err := decode(decoderRow{raw})
			return v.Name, err
		}).WithParameterType(codec.TypeText)
		return mutationTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Codec(c))}
	})
}

type decoderRow struct{ value any }

func (r decoderRow) Scan(dest ...any) error {
	*dest[0].(*int64) = 42
	*dest[1].(*string) = r.value.(string)
	return nil
}

func TestAutocommitModelDecoderFailuresRetainUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"Error-private", "Error-panic", "Error-goexit", "Format-private", "Format-panic", "Format-goexit", "callback-panic", "callback-goexit"} {
		t.Run(mode, func(t *testing.T) {
			state := &driverState{}
			var sends, begins atomic.Int64
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				sends.Add(1)
				return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(42), "private model"}}}, nil
			}
			state.begin = func(context.Context, driver.TxOptions) (driver.Tx, error) {
				begins.Add(1)
				return transaction{state}, nil
			}
			db := open(t, state, oneObserverConnection)
			parts := strings.Split(mode, "-")
			cause := &scopeFormattingError{method: parts[0], mode: parts[1]}
			model := autocommitDecoderModel(func(row database.Row) (mutationRecord, error) {
				var partial mutationRecord
				if err := row.Scan(&partial.ID, &partial.Name); err != nil {
					return mutationRecord{}, err
				}
				if parts[0] == "callback" {
					if parts[1] == "goexit" {
						runtime.Goexit()
					}
					panic("private decoder callback")
				}
				return partial, cause
			})
			result, err := orm.Insert(t.Context(), db, model.Table, orm.TextSet(model.Name, "private record name"))
			var outcome *database.Error
			var candidate *orm.WriteError[mutationRecord]
			if result != (mutationRecord{}) || !errors.Is(err, database.CommitUnknown) || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown || errors.As(err, &candidate) {
				t.Fatal("decoder failure published a partial candidate or lost unknown outcome")
			}
			if parts[0] == "callback" {
				if !errors.Is(err, fault.Panicked) {
					t.Fatal("decoder panic or Goexit was not contained")
				}
			} else {
				var retained *scopeFormattingError
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("decoder error identity lost")
				}
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.calls != 0 {
				t.Fatal("decoder error formatting escaped redaction")
			}
			if sends.Load() != 1 || begins.Load() != 0 || state.rowsClosed.Load() != 1 || state.committed.Load() != 0 || state.rolledBack.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("decoder failure retried, claimed a transaction outcome, or retained rows")
			}
			if _, err := db.Exec(t.Context(), "healthy after decoder failure"); err != nil {
				t.Fatal("pool unusable after decoder failure")
			}
		})
	}
}

func TestAutocommitDecoderAndCloseFailuresPreserveOutcomeAndCandidate(t *testing.T) {
	for _, decodeFails := range []bool{false, true} {
		t.Run(fmt.Sprint(decodeFails), func(t *testing.T) {
			closeCause := errors.New("lost statement completion")
			decodeCause := &scopeFormattingError{method: "Format", mode: "panic"}
			state := &driverState{}
			var sends atomic.Int64
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				sends.Add(1)
				return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(42), "private model"}}, closeError: closeCause}, nil
			}
			db := open(t, state, oneObserverConnection)
			model := autocommitDecoderModel(func(row database.Row) (mutationRecord, error) {
				var decoded mutationRecord
				if err := row.Scan(&decoded.ID, &decoded.Name); err != nil {
					return mutationRecord{}, err
				}
				if decodeFails {
					return decoded, decodeCause
				}
				return decoded, nil
			})
			result, err := orm.Insert(t.Context(), db, model.Table, orm.TextSet(model.Name, "private record name"))
			var outcome *database.Error
			var candidate *orm.WriteError[mutationRecord]
			if result != (mutationRecord{}) || !errors.Is(err, closeCause) || !errors.Is(err, database.CommitUnknown) || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown {
				t.Fatal("close failure lost its cause or unknown statement outcome")
			}
			if errors.As(err, &candidate) == decodeFails || !decodeFails && candidate.Candidate().ID != 42 {
				t.Fatal("reconciliation candidate did not reflect completed hydration")
			}
			if decodeFails && !errors.Is(err, decodeCause) {
				t.Fatal("row cleanup replaced the decoder failure")
			}
			if decodeCause.calls != 0 || sends.Load() != 1 || state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("combined failure formatted, retried or retained rows")
			}
		})
	}
}
