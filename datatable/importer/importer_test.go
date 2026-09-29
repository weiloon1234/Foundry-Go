package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type memberImport struct {
	Name     string
	Age      int64
	Balance  decimal.Decimal
	Joined   temporal.Date
	Seen     value.Nullable[temporal.DateTime]
	Nickname value.Nullable[string]
	Active   bool
}

func memberColumns() []Column[memberImport] {
	text := foundryhttp.StringQuery[string]()
	return []Column[memberImport]{
		Field("Name", text, func(row *memberImport, v string) { row.Name = v }),
		Field("Age", foundryhttp.IntegerQuery[int64](), func(row *memberImport, v int64) { row.Age = v }),
		Field("Balance", foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), func(row *memberImport, v decimal.Decimal) { row.Balance = v }),
		Field("Joined", foundryhttp.TextQuery[temporal.Date, *temporal.Date](), func(row *memberImport, v temporal.Date) { row.Joined = v }),
		NullableField("Seen", foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime](), func(row *memberImport, v value.Nullable[temporal.DateTime]) { row.Seen = v }),
		NullableField("Nickname", text, func(row *memberImport, v value.Nullable[string]) { row.Nickname = v }),
		Field("Active", foundryhttp.BoolQuery[bool](), func(row *memberImport, v bool) { row.Active = v }),
	}
}

func memberSpec() Spec[memberImport] {
	name := validation.DefineField("name", func(row memberImport) string { return row.Name })
	age := validation.DefineField("age", func(row memberImport) int64 { return row.Age })
	return Spec[memberImport]{
		Columns: memberColumns(),
		Rules:   []validation.Rule[memberImport]{name.Rules(validation.MinLength[string](2)), age.Rules(validation.Between[int64](0, 150))},
	}
}

type collected struct {
	chunks [][]Row[memberImport]
}

func (c *collected) handle(_ context.Context, rows []Row[memberImport]) error {
	c.chunks = append(c.chunks, rows)
	return nil
}
func (c *collected) rows() []Row[memberImport] {
	var all []Row[memberImport]
	for _, chunk := range c.chunks {
		all = append(all, chunk...)
	}
	return all
}

func TestCSVImportMapsHeadingsValidatesRowsAndChunks(t *testing.T) {
	spec := memberSpec()
	spec.Limits = DefaultLimits()
	spec.Limits.ChunkSize = 2
	definition := Define(spec)
	input := "\uFEFF name ,AGE,Balance,Joined,Active,Ignored,Nickname\n" +
		"Ada,36,1.25,2026-09-25,true,x,\n" +
		"\n" +
		"B,200,x,2026-02-30,maybe,x,Bee\n" +
		"Grace,85,9007199254740993.125,1906-12-09,false,x,\"Amazing\nGrace\"\n" +
		",,,,,,\n" +
		"Linus,55,0,2026-01-01,true,x,Tux\n"
	var out collected
	report, err := definition.Run(t.Context(), CSVSource(strings.NewReader(input)), out.handle)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.rows()
	if report.Rows != 4 || report.Imported != 3 || report.Failed != 1 || len(out.chunks) != 2 || len(rows) != 3 {
		t.Fatal("unexpected import summary", report, len(out.chunks))
	}
	if rows[0].Number != 2 || rows[0].Value.Name != "Ada" || rows[0].Value.Age != 36 || rows[0].Value.Balance.String() != "1.25" || !rows[0].Value.Nickname.IsNull() || !rows[0].Value.Seen.IsNull() || !rows[0].Value.Active {
		t.Fatal("first row lost typed values", rows[0])
	}
	if nickname, _ := rows[1].Value.Nickname.Get(); rows[1].Number != 5 || nickname != "Amazing\nGrace" || rows[1].Value.Balance.String() != "9007199254740993.125" {
		t.Fatal("quoted multi-line cell or exact decimal changed", rows[1])
	}
	failure := report.Failures[0]
	var paths []string
	for _, issue := range failure.Issues {
		paths = append(paths, issue.Path+":"+string(issue.Code))
	}
	if failure.Row != 4 || !reflect.DeepEqual(paths, []string{"/Balance:type", "/Joined:type", "/Active:type"}) {
		t.Fatal("parse failures lost row number or column paths", failure)
	}
}

func TestImportRunsRulesOnlyAfterSuccessfulParsing(t *testing.T) {
	definition := Define(memberSpec())
	input := "Name,Age,Balance,Joined,Active\nA,151,1,2026-01-01,true\nAl,,1,2026-01-01,true\n"
	var out collected
	report, err := definition.Run(t.Context(), CSVSource(strings.NewReader(input)), out.handle)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 2 || report.Imported != 0 || len(report.Failures) != 2 {
		t.Fatal("invalid rows were imported", report)
	}
	first, second := report.Failures[0], report.Failures[1]
	if first.Row != 2 || len(first.Issues) != 2 || first.Issues[0].Path != "/name" || first.Issues[1].Path != "/age" {
		t.Fatal("rule issues lost their field paths", first)
	}
	if second.Row != 3 || len(second.Issues) != 1 || second.Issues[0].Code != contract.RequiredIssue || second.Issues[0].Path != "/Age" {
		t.Fatal("empty required cell was not reported before rules", second)
	}
}

func TestImportHeadingsMustMatchTheDeclaration(t *testing.T) {
	definition := Define(memberSpec())
	for name, input := range map[string]string{
		"missing":  "Name,Age,Balance,Joined\nAda,1,1,2026-01-01\n",
		"repeated": "Name,Age,Balance,Joined,Active,age\nAda,1,1,2026-01-01,true,2\n",
	} {
		called := false
		report, err := definition.Run(t.Context(), CSVSource(strings.NewReader(input)), func(context.Context, []Row[memberImport]) error { called = true; return nil })
		var headings *HeadingError
		if !errors.As(err, &headings) || !errors.Is(err, fault.Invalid) || called || report.Rows != 0 || len(headings.Issues()) != 1 {
			t.Fatal("heading mismatch was not rejected before rows", name, err)
		}
	}
	if _, err := definition.Run(t.Context(), CSVSource(strings.NewReader("")), (&collected{}).handle); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty input accepted", err)
	}
	spec := memberSpec()
	spec.HeadingRow = 3
	input := "Monthly report\n\nName,Age,Balance,Joined,Active\nAda,36,1,2026-01-01,true\n"
	var out collected
	if report, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader(input)), out.handle); err != nil || report.Imported != 1 || out.rows()[0].Number != 4 {
		t.Fatal("heading row offset was not honored", report, err)
	}
	if report, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader("Name;Age;Balance;Joined;Active\n")).WithComma(';'), out.handle); !errors.Is(err, fault.Invalid) || report.Rows != 0 {
		t.Fatal("missing heading row accepted", err)
	}
	semicolon := "Name;Age;Balance;Joined;Active\nAda;36;1,5;2026-01-01;true\n"
	if report, err := Define(memberSpec()).Run(t.Context(), CSVSource(strings.NewReader(semicolon)).WithComma(';'), (&collected{}).handle); err != nil || report.Failed != 1 {
		t.Fatal("custom delimiter changed parsing", report, err)
	}
}

func TestImportDeclarationAndSourceValidation(t *testing.T) {
	text := foundryhttp.StringQuery[string]()
	for name, spec := range map[string]Spec[memberImport]{
		"no columns":        {},
		"repeated heading":  {Columns: []Column[memberImport]{Field("Name", text, func(*memberImport, string) {}), Field(" name", text, func(*memberImport, string) {})}},
		"missing assign":    {Columns: []Column[memberImport]{Field[memberImport, string]("Name", text, nil)}},
		"blank heading":     {Columns: []Column[memberImport]{Field(" ", text, func(*memberImport, string) {})}},
		"undefined column":  {Columns: []Column[memberImport]{{}}},
		"invalid limits":    {Columns: memberColumns(), Limits: Limits{MaxRows: 1}},
		"invalid time zone": {Columns: memberColumns(), TimeZone: "Mars/Olympus"},
		"negative heading":  {Columns: memberColumns(), HeadingRow: -1},
	} {
		if err := Define(spec).Validate(); err == nil {
			t.Fatal("invalid import declaration accepted", name)
		}
	}
	var undefined Definition[memberImport]
	if _, err := undefined.Run(t.Context(), CSVSource(strings.NewReader("")), (&collected{}).handle); err == nil {
		t.Fatal("undefined import ran")
	}
	definition := Define(memberSpec())
	for name, source := range map[string]Source{
		"empty":           {},
		"CSV sheet":       CSVSource(strings.NewReader("")).WithSheet("Data"),
		"XLSX delimiter":  XLSXSource(bytes.NewReader(nil), 0).WithComma(';'),
		"quote delimiter": CSVSource(strings.NewReader("")).WithComma('"'),
		"nil XLSX":        XLSXSource(nil, 0),
	} {
		if _, err := definition.Run(t.Context(), source, (&collected{}).handle); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid source accepted", name, err)
		}
	}
	if _, err := definition.Run(t.Context(), CSVSource(strings.NewReader("")), nil); err == nil {
		t.Fatal("missing handler accepted")
	}
}

func TestImportLimitsBoundRowsBytesAndFailures(t *testing.T) {
	var body strings.Builder
	body.WriteString("Name,Age,Balance,Joined,Active\n")
	for i := range 20 {
		fmt.Fprintf(&body, "Name%d,%d,1,2026-01-01,true\n", i, i)
	}
	input := body.String()
	for name, change := range map[string]func(*Limits){
		"rows":   func(l *Limits) { l.MaxRows = 10 },
		"input":  func(l *Limits) { l.MaxInputBytes = 64 },
		"record": func(l *Limits) { l.MaxRowBytes = 24; l.MaxCellBytes = 16 },
		"column": func(l *Limits) { l.MaxColumns = 4 },
	} {
		spec := memberSpec()
		spec.Limits = DefaultLimits()
		change(&spec.Limits)
		if _, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader(input)), (&collected{}).handle); !errors.Is(err, fault.Invalid) {
			t.Fatal("import limit was not enforced", name, err)
		}
	}
	spec := memberSpec()
	spec.Limits = DefaultLimits()
	spec.Limits.MaxInputBytes = int64(len(input))
	if report, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader(input)), (&collected{}).handle); err != nil || report.Imported != 20 {
		t.Fatal("input at the exact byte bound was rejected", err)
	}
	spec.Limits.MaxFailures = 2
	bad := strings.ReplaceAll(input, "true", "no")
	report, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader(bad)), (&collected{}).handle)
	if err != nil || report.Failed != 20 || len(report.Failures) != 2 {
		t.Fatal("failure retention was not bounded", report, err)
	}
	spec.Limits.MaxCellBytes = 4
	report, err = Define(spec).Run(t.Context(), CSVSource(strings.NewReader(input)), (&collected{}).handle)
	if err != nil || report.Failed != 20 || report.Failures[0].Issues[0].Code != contract.LengthIssue {
		t.Fatal("oversized cell was not a row issue", report, err)
	}
	if _, err := Define(memberSpec()).Run(t.Context(), CSVSource(strings.NewReader("Name,\"Age\n")), (&collected{}).handle); !errors.Is(err, fault.Invalid) {
		t.Fatal("malformed CSV accepted", err)
	}
}

func TestImportHandlerFailuresPanicsAndCancellationStopTheRun(t *testing.T) {
	input := "Name,Age,Balance,Joined,Active\nAda,36,1,2026-01-01,true\nGrace,85,1,2026-01-01,true\n"
	spec := memberSpec()
	spec.Limits = DefaultLimits()
	spec.Limits.ChunkSize = 1
	definition := Define(spec)
	stop := errors.New("destination failed")
	calls := 0
	report, err := definition.Run(t.Context(), CSVSource(strings.NewReader(input)), func(context.Context, []Row[memberImport]) error { calls++; return stop })
	if !errors.Is(err, stop) || calls != 1 || report.Imported != 0 {
		t.Fatal("handler failure did not stop the import", err)
	}
	if _, err := definition.Run(t.Context(), CSVSource(strings.NewReader(input)), func(context.Context, []Row[memberImport]) error { panic("private payload") }); !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private payload") {
		t.Fatal("handler panic was not contained", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	report, err = definition.Run(ctx, CSVSource(strings.NewReader(input)), func(context.Context, []Row[memberImport]) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || report.Imported != 1 {
		t.Fatal("cancellation did not stop after the handled chunk", report, err)
	}
}

// workbook builds a minimal XLSX archive from its parts.
func workbook(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, body := range parts {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

const (
	relsNS      = `http://schemas.openxmlformats.org/officeDocument/2006/relationships`
	packageRels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + relsNS + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`
)

func workbookParts(sheets map[string]string, shared string) map[string]string {
	var entries, relationships strings.Builder
	parts := map[string]string{"_rels/.rels": packageRels}
	names := []string{"Summary", "Members"}
	for i, name := range names {
		body, ok := sheets[name]
		if !ok {
			continue
		}
		id := fmt.Sprintf("rId%d", i+1)
		fmt.Fprintf(&entries, `<sheet name="%s" sheetId="%d" r:id="%s"/>`, name, i+1, id)
		fmt.Fprintf(&relationships, `<Relationship Id="%s" Type="%s/worksheet" Target="/xl/worksheets/sheet%d.xml"/>`, id, relsNS, i+1)
		parts[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)] = `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + body + `</sheetData></worksheet>`
	}
	relationships.WriteString(`<Relationship Id="rId9" Type="` + relsNS + `/sharedStrings" Target="sharedStrings.xml"/>`)
	relationships.WriteString(`<Relationship Id="rId8" Type="` + relsNS + `/hyperlink" Target="https://example.com/" TargetMode="External"/>`)
	parts["xl/workbook.xml"] = `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="` + relsNS + `"><sheets>` + entries.String() + `</sheets></workbook>`
	parts["xl/_rels/workbook.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + relationships.String() + `</Relationships>`
	parts["xl/sharedStrings.xml"] = `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + shared + `</sst>`
	return parts
}

func TestXLSXImportReadsSharedInlineNumericBooleanAndDateCells(t *testing.T) {
	shared := `<si><t>Name</t></si><si><t>Age</t></si><si><t>Balance</t></si><si><t>Joined</t></si><si><t>Seen</t></si><si><t>Active</t></si>` +
		`<si><r><rPr><b/></rPr><t>A</t></r><r><t xml:space="preserve">da_x000D_</t></r><rPh><t>ignored</t></rPh></si>`
	members := `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c></row>` +
		`<row r="3"><c r="A3" t="s"><v>6</v></c><c r="B3"><v>36</v></c><c r="C3"><v>0.3</v></c><c r="D3"><v>46290</v></c><c r="E3"><v>46290.25</v></c><c r="F3" t="b"><v>1</v></c></row>` +
		`<row r="4"><c r="A4" t="inlineStr"><is><t>Grace</t></is></c><c r="B4" t="str"><f>40+45</f><v>85</v></c><c r="C4"><v>1E-3</v></c><c r="D4"><v>2</v></c><c r="F4" t="e"><v>#N/A</v></c></row>` +
		`<row r="5"><c r="A5" t="inlineStr"><is><t>Linus</t></is></c><c r="B5"><v>55.5</v></c><c r="C5"><v>1</v></c><c r="D5"><v>46290</v></c><c r="F5" t="b"><v>0</v></c></row>`
	data := workbook(t, workbookParts(map[string]string{"Summary": `<row r="1"><c r="A1" t="inlineStr"><is><t>ignore</t></is></c></row>`, "Members": members}, shared))
	spec := memberSpec()
	spec.TimeZone = "Asia/Kuala_Lumpur"
	var out collected
	report, err := Define(spec).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))).WithSheet("Members"), out.handle)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.rows()
	if report.Rows != 3 || report.Imported != 1 || report.Failed != 2 || len(rows) != 1 {
		t.Fatal("unexpected XLSX summary", report)
	}
	row := rows[0]
	seen, _ := row.Value.Seen.Get()
	if row.Number != 3 || row.Value.Name != "Ada\r" || row.Value.Age != 36 || row.Value.Balance.String() != "0.3" || row.Value.Joined.String() != "2026-09-25" || !row.Value.Active {
		t.Fatal("XLSX cells lost their meaning", row)
	}
	if text, _ := seen.MarshalText(); string(text) != "2026-09-24T22:00:00Z" {
		t.Fatal("serial date-time ignored the import zone", string(text))
	}
	var paths []string
	for _, failure := range report.Failures {
		for _, issue := range failure.Issues {
			paths = append(paths, fmt.Sprintf("%d%s:%s", failure.Row, issue.Path, issue.Code))
		}
	}
	if !reflect.DeepEqual(paths, []string{"4/Joined:type", "4/Active:type", "5/Age:type"}) {
		t.Fatal("XLSX failures changed", paths)
	}
	var first collected
	if report, err := Define(memberSpec()).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))), first.handle); !errors.As(err, new(*HeadingError)) || report.Rows != 0 {
		t.Fatal("first worksheet was not selected by default", err)
	}
	if _, err := Define(memberSpec()).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))).WithSheet("Missing"), first.handle); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing worksheet accepted", err)
	}
}

func TestXLSXImportRejectsArchiveAndXMLBombs(t *testing.T) {
	heading := `<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row>`
	nameOnly := Spec[memberImport]{Columns: []Column[memberImport]{Field("Name", foundryhttp.StringQuery[string](), func(row *memberImport, v string) { row.Name = v })}}
	run := func(data []byte, limits Limits) error {
		spec := nameOnly
		spec.Limits = limits
		_, err := Define(spec).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))), (&collected{}).handle)
		return err
	}
	var rows strings.Builder
	rows.WriteString(heading)
	for i := 2; i < 2000; i++ {
		fmt.Fprintf(&rows, `<row r="%d"><c r="A%d" t="inlineStr"><is><t>%s</t></is></c></row>`, i, i, strings.Repeat("x", 100))
	}
	large := workbook(t, workbookParts(map[string]string{"Members": rows.String()}, ""))
	limits := DefaultLimits()
	if err := run(large, limits); err != nil {
		t.Fatal(err)
	}
	limits.MaxXMLBytes = 64 << 10
	if err := run(large, limits); !errors.Is(err, fault.Invalid) {
		t.Fatal("decompressed XML budget was not enforced", err)
	}
	limits = DefaultLimits()
	limits.MaxInputBytes = int64(len(large)) - 1
	if err := run(large, limits); !errors.Is(err, fault.Invalid) {
		t.Fatal("archive size was not bounded", err)
	}
	limits = DefaultLimits()
	limits.MaxCellBytes = 16
	token := workbook(t, workbookParts(map[string]string{"Members": heading + `<row r="2"><c r="A2" t="inlineStr"><is><t>` + strings.Repeat("y", 8192) + `</t></is></c></row>`}, ""))
	if err := run(token, limits); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized XML token was not bounded", err)
	}
	many := make(map[string]string, maxArchiveEntries+1)
	for part, body := range workbookParts(map[string]string{"Members": heading}, "") {
		many[part] = body
	}
	for i := range maxArchiveEntries {
		many[fmt.Sprintf("padding/%d.xml", i)] = "<x/>"
	}
	if err := run(workbook(t, many), DefaultLimits()); !errors.Is(err, fault.Invalid) {
		t.Fatal("central directory size was not bounded", err)
	}
	for name, sheet := range map[string]string{
		"row order":  heading + `<row r="1"><c r="A1"><v>1</v></c></row>`,
		"cell order": heading + `<row r="2"><c r="B2"><v>1</v></c><c r="A2"><v>1</v></c></row>`,
		"cell row":   heading + `<row r="2"><c r="A3"><v>1</v></c></row>`,
		"truncated":  heading + `<row r="2"><c r="A2"><v>1</v>`,
	} {
		data := workbook(t, workbookParts(map[string]string{"Members": sheet}, ""))
		if err := run(data, DefaultLimits()); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed worksheet accepted", name, err)
		}
	}
	if err := run([]byte("not a zip archive at all, only text"), DefaultLimits()); !errors.Is(err, fault.Invalid) {
		t.Fatal("non-archive accepted", err)
	}
}

// Numbers a spreadsheet would show rounded are type issues, never silently
// rounded values: a 16-digit identifier stored as a number cannot be trusted.
func TestXLSXImportRejectsNumbersThatFifteenDigitsCannotRepresent(t *testing.T) {
	type measured struct {
		ID     int64
		Label  string
		Ratio  float64
		Amount decimal.Decimal
	}
	spec := Spec[measured]{Columns: []Column[measured]{
		Field("ID", foundryhttp.IntegerQuery[int64](), func(row *measured, v int64) { row.ID = v }),
		Field("Label", foundryhttp.StringQuery[string](), func(row *measured, v string) { row.Label = v }),
		Field("Ratio", foundryhttp.FloatQuery[float64](), func(row *measured, v float64) { row.Ratio = v }),
		Field("Amount", foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), func(row *measured, v decimal.Decimal) { row.Amount = v }),
	}}
	heading := `<row r="1"><c r="A1" t="inlineStr"><is><t>ID</t></is></c><c r="B1" t="inlineStr"><is><t>Label</t></is></c><c r="C1" t="inlineStr"><is><t>Ratio</t></is></c><c r="D1" t="inlineStr"><is><t>Amount</t></is></c></row>`
	sheet := heading +
		`<row r="2"><c r="A2"><v>123456789012345</v></c><c r="B2"><v>123456789012345</v></c><c r="C2"><v>0.30000000000000004</v></c><c r="D2"><v>1.5</v></c></row>` +
		`<row r="3"><c r="A3"><v>1234567890123456</v></c><c r="B3"><v>1234567890123456</v></c><c r="C3"><v>1</v></c><c r="D3"><v>0.30000000000000004</v></c></row>` +
		`<row r="4"><c r="A4"><v>1.2345678901234568E+18</v></c><c r="B4" t="inlineStr"><is><t>1234567890123456</t></is></c><c r="C4"><v>2</v></c><c r="D4"><v>2</v></c></row>`
	data := workbook(t, workbookParts(map[string]string{"Members": sheet}, ""))
	var imported []measured
	report, err := Define(spec).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))), func(_ context.Context, rows []Row[measured]) error {
		for _, row := range rows {
			imported = append(imported, row.Value)
		}
		return nil
	})
	if err != nil || report.Imported != 1 || report.Failed != 2 || len(imported) != 1 {
		t.Fatal("unexpected import summary", report, err)
	}
	if got := imported[0]; got.ID != 123456789012345 || got.Label != "123456789012345" || got.Ratio != 0.30000000000000004 || got.Amount.String() != "1.5" {
		t.Fatal("exact numbers changed", got)
	}
	var paths []string
	for _, failure := range report.Failures {
		for _, issue := range failure.Issues {
			paths = append(paths, fmt.Sprintf("%d%s:%s", failure.Row, issue.Path, issue.Code))
		}
	}
	if !reflect.DeepEqual(paths, []string{"3/ID:type", "3/Label:type", "3/Amount:type", "4/ID:type"}) {
		t.Fatal("rounded numbers were not reported", paths)
	}
}

// A row made only of error cells or broken shared-string references is not
// blank: it is reported as a failure instead of disappearing.
func TestXLSXImportReportsRowsOfErrorCells(t *testing.T) {
	nameOnly := Spec[memberImport]{Columns: []Column[memberImport]{Field("Name", foundryhttp.StringQuery[string](), func(row *memberImport, v string) { row.Name = v })}}
	sheet := `<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row>` +
		`<row r="2"><c r="A2" t="e"><v>#REF!</v></c></row>` +
		`<row r="3"><c r="A3" t="s"><v>99</v></c></row>` +
		`<row r="4"><c r="B4" t="e"><v>#N/A</v></c></row>` +
		`<row r="5"><c r="A5"/></row>` +
		`<row r="6"><c r="A6" t="inlineStr"><is><t>Ada</t></is></c></row>`
	data := workbook(t, workbookParts(map[string]string{"Members": sheet}, ""))
	var out collected
	report, err := Define(nameOnly).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))), out.handle)
	if err != nil || report.Rows != 4 || report.Imported != 1 || report.Failed != 3 {
		t.Fatal("error-only rows were skipped as blank", report, err)
	}
	var paths []string
	for _, failure := range report.Failures {
		for _, issue := range failure.Issues {
			paths = append(paths, fmt.Sprintf("%d%s:%s", failure.Row, issue.Path, issue.Code))
		}
	}
	if !reflect.DeepEqual(paths, []string{"2/Name:type", "3/Name:type", "4/Name:required"}) {
		t.Fatal("error rows lost their issues", paths)
	}
}

// '>' is legal in XML text and attribute values, and '<' in comments, CDATA and
// processing instructions. None of them may let one token outgrow the bound,
// in a worksheet or in the shared-string table.
func TestXLSXImportBoundsTokensWhateverDelimitersTheyContain(t *testing.T) {
	heading := `<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row>`
	nameOnly := Spec[memberImport]{Columns: []Column[memberImport]{Field("Name", foundryhttp.StringQuery[string](), func(row *memberImport, v string) { row.Name = v })}}
	run := func(sheet, shared string) (Report, []Row[memberImport], error) {
		data := workbook(t, workbookParts(map[string]string{"Members": sheet}, shared))
		if len(data) > 64<<10 {
			t.Fatal("test bomb is not compressed", len(data))
		}
		var out collected
		report, err := Define(nameOnly).Run(t.Context(), XLSXSource(bytes.NewReader(data), int64(len(data))), out.handle)
		return report, out.rows(), err
	}
	report, rows, err := run(heading+`<row r="2"><c r="A2" t="inlineStr"><is><t>a&gt;b>c</t></is></c></row>`+`<!-- a < b > c --><?pi a<b>c?>`+`<row r="3"><c r="A3" t="inlineStr"><is><t><![CDATA[x<y>z]]></t></is></c></row>`, "")
	if err != nil || report.Imported != 2 || rows[0].Value.Name != "a>b>c" || rows[1].Value.Name != "x<y>z" {
		t.Fatal("ordinary delimiters inside tokens were rejected", report, err)
	}
	bomb := strings.Repeat("a>", 1<<20)
	for name, parts := range map[string][2]string{
		"text":          {heading + `<row r="2"><c r="A2" t="inlineStr"><is><t>` + bomb + `</t></is></c></row>`, ""},
		"shared string": {heading + `<row r="2"><c r="A2" t="s"><v>0</v></c></row>`, `<si><t>` + bomb + `</t></si>`},
		"attribute":     {heading + `<row r="2" x="` + bomb + `"><c r="A2" t="inlineStr"><is><t>a</t></is></c></row>`, ""},
		"cdata":         {heading + `<row r="2"><c r="A2" t="inlineStr"><is><t><![CDATA[` + strings.Repeat("a<", 1<<20) + `]]></t></is></c></row>`, ""},
		"comment":       {heading + `<!--` + strings.Repeat("a<b>", 1<<19) + `-->`, ""},
		"instruction":   {heading + `<?pi ` + strings.Repeat("a<b>", 1<<19) + `?>`, ""},
	} {
		if _, _, err := run(parts[0], parts[1]); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "oversized value") {
			t.Fatal("unbounded XML token accepted", name, err)
		}
	}
	if _, _, err := run(`<!DOCTYPE x [<!ENTITY a "b">]>`+heading, ""); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "document type declaration") {
		t.Fatal("document type declaration accepted", err)
	}
	if DefaultLimits().MaxXMLBytes != 128<<20 {
		t.Fatal("decompressed XML default changed")
	}
}
