package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestModelPaginationValidatesBeforeExecution(t *testing.T) {
	state := mutationDriver(nil)
	var calls atomic.Int64
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		calls.Add(1)
		return nil, errors.New("unexpected query")
	}
	db := open(t, state, nil)
	if _, err := mutationModel().Paginate(t.Context(), db, query.PageRequest{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid page accepted")
	}
	if _, err := mutationModel().Limit(0).Paginate(t.Context(), db, query.PageRequest{Number: 1, Size: 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal("window was discarded")
	}
	if _, err := mutationModel().CursorPaginate(t.Context(), db, query.CursorRequest[mutationRecord]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid cursor page accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mutationModel().Paginate(ctx, db, query.PageRequest{Number: 1, Size: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled page accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request touched database")
	}
}

func TestModelPaginationDiscardsPartialResultOnFailure(t *testing.T) {
	for _, kind := range []string{"count", "row", "close", "negative count"} {
		t.Run(kind, func(t *testing.T) {
			state := &driverState{}
			cause := errors.New("query stage failed")
			var rowQueries atomic.Int64
			state.query = func(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.HasPrefix(sql, "SELECT COUNT") {
					if kind == "count" {
						return nil, cause
					}
					count := int64(2)
					if kind == "negative count" {
						count = -1
					}
					return &resultRows{state: state, values: [][]driver.Value{{count}}}, nil
				}
				rowQueries.Add(1)
				rows := &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "complete"}, {int64(2), "second"}}}
				if kind == "row" {
					rows.values[1][0] = "malformed"
				}
				if kind == "close" {
					rows.closeError = cause
				}
				return rows, nil
			}
			db := open(t, state, nil)
			page, err := mutationModel().Paginate(t.Context(), db, query.PageRequest{Number: 1, Size: 2})
			if err == nil || page.Items != nil || page.Total != 0 || page.Number != 0 {
				t.Fatal("failed pagination published partial models/metadata")
			}
			if (kind == "count" || kind == "negative count") && rowQueries.Load() != 0 {
				t.Fatal("rows executed after failed count")
			}
		})
	}
}
