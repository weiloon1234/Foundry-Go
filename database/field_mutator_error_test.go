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

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestFieldMutatorsContainFailuresBeforeWriting(t *testing.T) {
	for _, operation := range []string{"insert", "patch"} {
		for _, mode := range []string{"Error-private", "Error-panic", "Error-goexit", "Format-private", "Format-panic", "Format-goexit", "callback-panic", "callback-goexit"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				state := &driverState{}
				var sends, begins atomic.Int64
				state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
					sends.Add(1)
					return nil, errors.New("unexpected write")
				}
				state.begin = func(context.Context, driver.TxOptions) (driver.Tx, error) {
					begins.Add(1)
					return transaction{state}, nil
				}
				db := open(t, state, oneObserverConnection)
				parts := strings.Split(mode, "-")
				cause := &scopeFormattingError{method: parts[0], mode: parts[1]}
				calls := 0
				type fields struct {
					orm.Table[mutationRecord, int64]
					Name orm.InputText[mutationRecord, string, string]
				}
				model := orm.Define("records", func(b orm.Builder[mutationRecord], m *mutationRecord) fields {
					return fields{orm.TableOf(b, &m.ID), orm.InputTextOf(b, &m.Name, func(string) (string, error) {
						calls++
						if parts[0] == "callback" {
							if parts[1] == "goexit" {
								runtime.Goexit()
							}
							panic("private mutator callback")
						}
						return "private partial output", cause
					})}
				})
				var result mutationRecord
				var err error
				if operation == "insert" {
					result, err = model.Insert(t.Context(), db, model.Name.Set("private record name"))
				} else {
					result, err = model.Update(t.Context(), db, 1, model.Name.Set("private record name"))
				}
				if result != (mutationRecord{}) || err == nil || calls != 1 || sends.Load() != 0 {
					t.Fatal("mutator failure published a result, retried, or sent a write")
				}
				if parts[0] == "callback" {
					if !errors.Is(err, fault.Panicked) {
						t.Fatal("mutator panic or Goexit escaped containment")
					}
				} else {
					var retained *scopeFormattingError
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("mutator error identity lost")
					}
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.calls != 0 {
					t.Fatal("mutator failure invoked application formatting")
				}
				// Native input mutators run under the write transaction owner.
				wantTransactions := int64(1)
				if begins.Load() != wantTransactions || state.rolledBack.Load() != wantTransactions || state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("mutator failure committed or retained transaction ownership")
				}
			})
		}
	}
}
