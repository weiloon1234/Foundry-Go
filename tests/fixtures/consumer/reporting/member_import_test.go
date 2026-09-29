package reporting_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable/importer"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestMemberImportMapsTypedRowsAndReportsFailures(t *testing.T) {
	upload := "Balance,name,State,Nickname,Notes\n" +
		"1.25,Ada,active,,first\n" +
		"x,Grace,retired,Amazing,second\n" +
		"9007199254740993.125,Linus,disabled,Tux,third\n" +
		"0,   ,active,,fourth\n"
	var imported []importer.Row[reporting.MemberImport]
	report, err := reporting.MemberImports.Run(t.Context(), importer.CSVSource(strings.NewReader(upload)), func(_ context.Context, rows []importer.Row[reporting.MemberImport]) error {
		imported = append(imported, rows...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != 4 || report.Imported != 2 || report.Failed != 2 || len(imported) != 2 {
		t.Fatal("unexpected import summary", report)
	}
	first, second := imported[0], imported[1]
	if first.Number != 2 || first.Value.Name != "Ada" || !first.Value.Nickname.IsNull() || first.Value.State != reporting.Active || first.Value.Balance.String() != "1.25" {
		t.Fatal("first member lost typed values", first)
	}
	if nickname, _ := second.Value.Nickname.Get(); second.Number != 4 || nickname != "Tux" || second.Value.State != reporting.Disabled || second.Value.Balance.String() != "9007199254740993.125" {
		t.Fatal("exact decimal or enum changed", second)
	}
	parse, rule := report.Failures[0], report.Failures[1]
	if parse.Row != 3 || len(parse.Issues) != 2 || parse.Issues[0].Path != "/State" || parse.Issues[1].Path != "/Balance" || parse.Issues[0].Code != contract.TypeIssue {
		t.Fatal("parse failures lost their row or column", parse)
	}
	if rule.Row != 5 || len(rule.Issues) != 1 || rule.Issues[0].Path != "/name" {
		t.Fatal("row rule failure lost its field path", rule)
	}
	_, err = reporting.MemberImports.Run(t.Context(), importer.CSVSource(strings.NewReader("Name,Nickname\nAda,\n")), func(context.Context, []importer.Row[reporting.MemberImport]) error {
		t.Error("rows reached the handler despite missing headings")
		return nil
	})
	var headings *importer.HeadingError
	if !errors.As(err, &headings) || !errors.Is(err, fault.Invalid) || len(headings.Issues()) != 2 {
		t.Fatal("missing required headings were not reported", err)
	}
}
