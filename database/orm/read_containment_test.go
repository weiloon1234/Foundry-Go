package orm

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type containedReadRow struct {
	ID   int64
	Name string
}
type containedReadTable struct {
	Table[containedReadRow, int64]
	Name Text[containedReadRow, string]
}

func TestAllDiscardsBatchAndClosesRowsAfterApplicationDecodeFailure(t *testing.T) {
	for _, mode := range []string{"error", "invalid", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &hostileQueryCallbackError{mode: "exit"}
			var active atomic.Int32
			failed := true
			calls := 0
			c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(raw any) (string, error) {
				active.Add(1)
				defer active.Add(-1)
				calls++
				v := raw.(string)
				if failed && v == "second" {
					switch mode {
					case "error":
						return "partial", cause
					case "invalid":
						return "partial", fault.Wrap(fault.Invalid, "private classification", cause)
					case "exit":
						runtime.Goexit()
					default:
						panic(cause)
					}
				}
				return v, nil
			})
			table := Define("contained_reads", func(b Builder[containedReadRow], row *containedReadRow) containedReadTable {
				return containedReadTable{Table: TableOf(b, &row.ID), Name: TextOf(b, &row.Name, Codec(c))}
			})
			closed := 0
			db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "first"}, {int64(2), "second"}, {int64(3), "third"}}, onClose: func() { closed++ }})
			for range 10 {
				calls = 0
				before := closed
				items, err := All(t.Context(), db, table.Query())
				if err == nil || items != nil || calls != 2 || active.Load() != 0 || closed != before+1 {
					t.Fatal("partial batch escaped or callback/rows ownership leaked")
				}
				if errors.Is(err, fault.Panicked) != (mode == "panic" || mode == "exit") || errors.Is(err, fault.Invalid) != (mode == "invalid") {
					t.Fatal("classification changed")
				}
				if mode == "error" || mode == "invalid" {
					var retained *hostileQueryCallbackError
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("decode identity lost")
					}
				}
				if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, "private") || cause.calls != 0 {
					t.Fatal("application text formatted")
				}
			}
			failed = false
			items, err := All(t.Context(), db, table.Query())
			if err != nil || len(items) != 3 || items[0].ID != 1 || items[0].Name != "first" || items[1].Name != "second" || items[2].Name != "third" {
				t.Fatal("scratch row alias or unhealthy pool after failure")
			}
		})
	}
}
