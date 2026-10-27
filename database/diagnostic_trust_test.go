package database

import (
	"bytes"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestStructuredDiagnosticSnapshotsRejectApplicationText(t *testing.T) {
	raw := &Error{operation: "execute", detail: Detail{Code: QueryFailed, SQLState: "password=hunter2", Constraint: "private constraint"}, outcome: NoCommit}
	err := presentFailure(raw)
	before := errordiag.Describe(err)
	raw.operation = "password=hunter2"
	raw.detail.Code = Code("password=hunter2")
	raw.outcome = Outcome("password=hunter2")
	after := errordiag.Describe(err)
	if fmt.Sprint(before.Attributes) != fmt.Sprint(after.Attributes) || strings.Contains(fmt.Sprint(after), "hunter2") || strings.Contains(fmt.Sprint(after), "private constraint") {
		t.Fatal("database attributes were mutable or trusted adapter metadata")
	}
	if _, ok := safeerror.OwnedMessage(err); !ok {
		t.Fatal("missing construction snapshot")
	}
	private := NewError("password=hunter2", Code("password=hunter2"))
	if strings.Contains(fmt.Sprint(errordiag.Describe(private)), "hunter2") {
		t.Fatal("public database error became diagnostic authority")
	}
}

func TestStartupReportRejectsPrivateAdapterMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	failure := &Error{operation: "start", detail: Detail{Code: Code("password=hunter2"), SQLState: "MYKEY"}}
	startupLog(t.Context(), logger, slog.LevelError, "database startup failed", PrimaryPool, 1, time.Now(), 0, failure)
	if strings.Contains(output.String(), "hunter2") || strings.Contains(output.String(), "MYKEY") || !strings.Contains(output.String(), "query_failed") {
		t.Fatal("startup log trusted arbitrary adapter classification")
	}
}
