package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

type reportDriverFailure struct{ formatted atomic.Int32 }

func (e *reportDriverFailure) Error() string { e.formatted.Add(1); return "private driver diagnostic" }

func TestTransactionReportRetainsIndependentRollbackDiagnostic(t *testing.T) {
	query, rollback := &reportDriverFailure{}, &reportDriverFailure{}
	state := &driverState{rollbackError: rollback}
	state.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, query }
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(err error) database.Detail {
		if errors.Is(err, rollback) {
			return database.Detail{Code: database.Unavailable}
		}
		return database.Detail{Code: database.UniqueViolation}
	}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	err = db.Transaction(t.Context(), func(tx *database.Tx) error { _, err := tx.Exec(t.Context(), "failing statement"); return err })
	if !errors.Is(err, query) || !errors.Is(err, rollback) {
		t.Fatal("transaction lost independent causes")
	}
	report := errordiag.Describe(err)
	code, cleanup := false, false
	for _, a := range report.Attributes {
		if strings.Contains(a.Value, "private") {
			t.Fatal("application text in report")
		}
		if a.Key == "database_code" && a.Value == string(database.UniqueViolation) {
			code = true
		}
		if a.Key == "database_code" && a.Value == string(database.Unavailable) {
			cleanup = true
		}
	}
	if !code || !cleanup || query.formatted.Load() != 0 || rollback.formatted.Load() != 0 {
		t.Fatal("independent rollback classification hidden", report.Attributes)
	}
}
