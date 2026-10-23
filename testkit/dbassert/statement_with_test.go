package dbassert

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Exercise real failed Exec and Query completions, rather than supplying observer
// events by hand: rejected SQL is still a source of diagnostic text.
func TestStatementWithUtilitiesFailClosedAcrossDiagnostics(t *testing.T) {
	d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	db, err := database.Open(t.Context(), database.Adapter{Connector: shapeFailureConnector{&calls}}, database.DefaultPoolConfig(), database.WithStatementDetector(d))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	failStatements := func(ctx context.Context, sql string) {
		t.Helper()
		before := calls.Load()
		if _, err := db.Exec(ctx, sql); err == nil {
			t.Fatal("Exec unexpectedly succeeded")
		}
		rows, err := db.Query(ctx, sql)
		if err == nil || rows != nil {
			t.Fatal("Query unexpectedly published rows")
		}
		if calls.Load() != before+2 {
			t.Fatal("both failed statements must reach the driver")
		}
	}
	prefixes := []string{
		"WITH c AS (SELECT 1) ",
		" /* leading */ WITH\nRECURSIVE c(id) AS (SELECT 1)\t",
		"WITH c AS MATERIALIZED (SELECT 1), d AS NOT MATERIALIZED (VALUES (2)) /* main */ ",
		"WITH c AS (WITH nested AS (SELECT 1) SELECT * FROM nested) ",
		"WITH c AS (SELECT 1) , /* incomplete CTE */ ",
	}
	commands := []string{"SET app.token TO %s", "RESET %s", "SHOW %s", "COPY %s", "DO %s", "CALL %s", "EXPLAIN %s", "PREPARE %s", "EXECUTE %s", "LISTEN %s", "NOTIFY %s"}
	var cases []string
	for _, prefix := range prefixes {
		for _, command := range commands {
			for _, v := range []string{"canary_token", `"canary_token"`, "'canary_token'"} {
				utility := fmt.Sprintf(command, v)
				cases = append(cases, prefix+utility, "WITH outer_cte AS ("+prefix+utility+") SELECT * FROM outer_cte")
			}
		}
	}
	for _, end := range []string{"\r", "\n", "\r\n"} {
		for _, v := range []string{"canary_token", `"canary_token"`, "'canary_token'"} {
			cases = append(cases, "SELECT 1 --comment"+end+"SET app.token TO "+v,
				"SELECT (SET app.token TO "+v+")", "UPDATE records SET x=1 SET app.token TO "+v,
				"WITH c AS (SELECT 1 --comment"+end+") SET app.token TO "+v)
		}
	}
	for _, sql := range cases {
		ctx, scope := d.Begin(t.Context())
		failStatements(ctx, sql)
		report := scope.Report()
		if len(report.Violations) != 1 || !strings.HasPrefix(report.Violations[0].Shape, "untrusted statement ") || report.Violations[0].Count != 2 || strings.Contains(fmt.Sprint(report), "canary") {
			t.Fatalf("unsafe report: %+v", report)
		}
		reporter := &shapeTest{}
		assertStatementShapes(reporter, scope)
		if len(reporter.errors) != 1 || strings.Contains(strings.Join(reporter.errors, " "), "canary") {
			t.Fatalf("unsafe assertion: %v", reporter.errors)
		}
		var logs bytes.Buffer
		handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			failStatements(r.Context(), sql)
			w.WriteHeader(204)
		})
		wrapped, err := foundryhttp.ApplyMiddleware(handler, foundryhttp.StatementDetector(d, slog.New(slog.NewJSONHandler(&logs, nil))))
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		wrapped.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
		got := logs.String()
		if response.Code != 204 || !strings.Contains(got, "untrusted statement") || !strings.Contains(got, "call_sites") || strings.Contains(got, "canary") {
			t.Fatalf("unsafe warning: %s", got)
		}
	}

}

type shapeFailureConnector struct{ calls *atomic.Int64 }

func (c shapeFailureConnector) Connect(context.Context) (driver.Conn, error) {
	return shapeFailureConn{c.calls}, nil
}
func (c shapeFailureConnector) Driver() driver.Driver { return c }
func (c shapeFailureConnector) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector required")
}

type shapeFailureConn struct{ calls *atomic.Int64 }

func (shapeFailureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (shapeFailureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions unsupported")
}
func (shapeFailureConn) Close() error               { return nil }
func (shapeFailureConn) Ping(context.Context) error { return nil }
func (c shapeFailureConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.calls.Add(1)
	return nil, errors.New("syntax error near canary_token")
}
func (c shapeFailureConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.calls.Add(1)
	return nil, errors.New("syntax error near canary_token")
}
