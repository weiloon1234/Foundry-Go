package datatable

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/datatable/importer"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type sheetCell struct {
	Reference string  `xml:"r,attr"`
	Type      string  `xml:"t,attr"`
	Style     string  `xml:"s,attr"`
	Formula   *string `xml:"f"`
	Text      string  `xml:"is>t"`
	Value     string  `xml:"v"`
}
type sheetRow struct {
	Number int         `xml:"r,attr"`
	Cells  []sheetCell `xml:"c"`
}
type sheetDocument struct {
	XMLName xml.Name
	Rows    []sheetRow `xml:"sheetData>row"`
}

// Parse the archive and XML with independent standard readers. The transport
// adapter never shares its writer or escaping implementation with this reader.
func readWorkbook(t testing.TB, data []byte) (map[string][]byte, sheetDocument) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal("invalid completed ZIP", err)
	}
	parts := make(map[string][]byte)
	for _, file := range reader.File {
		if _, duplicate := parts[file.Name]; duplicate {
			t.Fatal("duplicate workbook part")
		}
		if file.UncompressedSize64 > 16<<20 {
			t.Fatal("unexpected unbounded test workbook part")
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(stream, 16<<20))
		closeErr := stream.Close()
		if err != nil || closeErr != nil {
			t.Fatal("workbook checksum/stream failure", err, closeErr)
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal(body, &root); err != nil {
			t.Fatal("malformed XML part", file.Name, err)
		}
		parts[file.Name] = body
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		if parts[name] == nil {
			t.Fatal("missing workbook part", name)
		}
	}
	if len(parts) != 6 {
		t.Fatal("unexpected external, macro or shared-string part")
	}
	var sheet sheetDocument
	if err := xml.Unmarshal(parts["xl/worksheets/sheet1.xml"], &sheet); err != nil {
		t.Fatal(err)
	}
	if sheet.XMLName.Space != "http://schemas.openxmlformats.org/spreadsheetml/2006/main" || sheet.XMLName.Local != "worksheet" {
		t.Fatal("invalid worksheet namespace")
	}
	return parts, sheet
}

var spreadsheetEscape = regexp.MustCompile(`_x([0-9a-fA-F]{4})_`)

func decodeSpreadsheetText(text string) string {
	// OOXML replacement is single-pass: escaped underscores do not recursively
	// decode a literal substring which only looks like a spreadsheet escape.
	return spreadsheetEscape.ReplaceAllStringFunc(text, func(match string) string {
		n, _ := strconv.ParseUint(match[2:6], 16, 16)
		return string(rune(n))
	})
}

func xlsxLayout() reportLayout {
	return reportLayout{format: XLSX, location: time.UTC, maxXMLBytes: DefaultConfig().MaxXMLBytes}
}

func TestXLSXStructureLiteralCellsAndExactValues(t *testing.T) {
	input := []string{"=SUM(A1:A2)", "+1", "-2", "@link", "9007199254740993", "12345678901234567890.001", "a<&>\"b", "\t padded \n", "line\rnext", "_x000D_", "_x005F_x0041_", "中文😀", "", "bell\x07\x00\x1b\x7f", "\ufffe\uffff", "_x\x01"}
	var output bytes.Buffer
	writer, err := newReportWriter(&output, xlsxLayout(), []string{"Name", "=heading"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	if err := writer.Row(textCells(input...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	parts, sheet := readWorkbook(t, output.Bytes())
	if len(sheet.Rows) != 2 || len(sheet.Rows[1].Cells) != len(input) {
		t.Fatal("worksheet dimensions changed")
	}
	var got []string
	for i, cell := range sheet.Rows[1].Cells {
		if cell.Type != "inlineStr" || cell.Formula != nil || cell.Reference != string(rune('A'+i))+"2" {
			t.Fatal("cell was interpreted or misaddressed", cell)
		}
		got = append(got, decodeSpreadsheetText(cell.Text))
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatal("exact text values changed", got)
	}
	for _, row := range sheet.Rows {
		if row.Number < 1 {
			t.Fatal("invalid row address")
		}
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	for name, want := range map[string][]string{"_rels/.rels": {"xl/workbook.xml"}, "xl/_rels/workbook.xml.rels": {"worksheets/sheet1.xml", "styles.xml"}} {
		relationships.Items = nil
		if err := xml.Unmarshal(parts[name], &relationships); err != nil || len(relationships.Items) != len(want) {
			t.Fatal("invalid workbook relationship", err)
		}
		for i, item := range relationships.Items {
			if item.ID != "rId"+strconv.Itoa(i+1) || item.Target != want[i] || item.Mode != "" {
				t.Fatal("external or broken relationship", item)
			}
		}
	}
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(parts["xl/workbook.xml"], &workbook); err != nil || len(workbook.Sheets) != 1 || workbook.Sheets[0].ID != "rId1" || workbook.Sheets[0].Name != "Report" {
		t.Fatal("worksheet not linked from workbook", err)
	}
	var repeat bytes.Buffer
	again, err := newReportWriter(&repeat, xlsxLayout(), []string{"Name", "=heading"})
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Row(textCells(input...)); err != nil {
		t.Fatal(err)
	}
	if err := again.Finish(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), repeat.Bytes()) {
		t.Fatal("identical report content produced nondeterministic archives")
	}
}

func TestXLSXRejectsLimitsAndFinalizationFailures(t *testing.T) {
	if _, err := newXLSXReport(io.Discard, 1, time.UTC); err == nil {
		t.Fatal("metadata escaped the XML budget")
	}
	writer, err := newXLSXReport(io.Discard, 1<<20, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	writer.row = 1_048_575
	if err := writer.Row(textCells("last")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(nil); err == nil {
		t.Fatal("worksheet row limit ignored")
	}
	writer.row = 0
	if err := writer.Row(make([]exportCell, 16385)); err == nil {
		t.Fatal("worksheet column limit ignored")
	}
	for n, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 702: "ZZ", 703: "AAA", 16384: "XFD"} {
		if got := xlsxColumn(n); got != want {
			t.Fatal("incorrect Excel column", n, got)
		}
	}
	writer.sheet = &xmlBudget{writer: io.Discard, remaining: 0}
	if err := writer.Finish(); err == nil {
		t.Fatal("closing XML escaped its budget")
	}
	if err := writer.Finish(); err == nil {
		t.Fatal("finalized writer accepted another finish")
	}
	stop := errors.New("archive output failed")
	output := &failureWriter{err: stop}
	broken, err := newXLSXReport(output, 1<<20, time.UTC)
	if err == nil {
		err = broken.Finish()
		broken.Abort()
	}
	if !errors.Is(err, stop) {
		t.Fatal("ZIP finalization hid output failure", err)
	}
}

type streamSink struct {
	bytes   int64
	writes  int
	largest int
}

func (s *streamSink) Write(p []byte) (int, error) {
	s.bytes += int64(len(p))
	s.writes++
	s.largest = max(s.largest, len(p))
	return len(p), nil
}

func TestXLSXWritesWhileRowsAreStillBeingProduced(t *testing.T) {
	sink := new(streamSink)
	writer, err := newXLSXReport(sink, 16<<20, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	initial := sink.bytes
	for row := range 4096 {
		digest := sha256.Sum256([]byte(strconv.Itoa(row)))
		if err := writer.Row(textCells(hex.EncodeToString(digest[:]), strings.Repeat("x", 256))); err != nil {
			t.Fatal(err)
		}
	}
	if sink.bytes <= initial+32<<10 {
		t.Fatal("worksheet output was deferred until finalization")
	}
	if sink.largest > 1<<20 {
		t.Fatal("workbook-sized output buffer observed")
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
}

func FuzzXLSXLiteralRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "_x000D_\r_x005F_", "_x0000\r0", "_x00\r00_", "_\rx0000_", "=SUM(A1:A2)", "中文😀<&>\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 || !utf8.ValidString(text) {
			t.Skip()
		}
		var output bytes.Buffer
		writer, err := newReportWriter(&output, xlsxLayout(), []string{"Value"})
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Abort()
		if err := writer.Row(textCells(text)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Finish(); err != nil {
			t.Fatal(err)
		}
		_, sheet := readWorkbook(t, output.Bytes())
		if len(sheet.Rows) != 2 || len(sheet.Rows[1].Cells) != 1 || decodeSpreadsheetText(sheet.Rows[1].Cells[0].Text) != text {
			t.Fatal("literal cell failed round trip")
		}
	})
}

func TestXLSXDoesNotRetainReportSizedCellBuffers(t *testing.T) {
	writer, err := newXLSXReport(io.Discard, 256<<20, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	// Distinct cells total more than 50 MiB. Sample live heap after collection
	// while the worksheet remains open, so a retained row/shared-string table
	// cannot hide behind finalization. This is not a process RSS quota.
	runtime.GC()
	var baseline, sample runtime.MemStats
	runtime.ReadMemStats(&baseline)
	padding := strings.Repeat("value", 210)
	var peak uint64
	for row := range 50_000 {
		if err := writer.Row(textCells(strconv.Itoa(row) + ":" + padding)); err != nil {
			t.Fatal(err)
		}
		if (row+1)%10_000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&sample)
			if sample.HeapAlloc > baseline.HeapAlloc {
				peak = max(peak, sample.HeapAlloc-baseline.HeapAlloc)
			}
		}
	}
	if peak > 8<<20 {
		t.Fatal("streaming writer retained report-sized data", peak)
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	t.Logf("peak additional live heap at checkpoints: %d bytes for 50,000 distinct cells", peak)
}

func BenchmarkXLSXStreamingRows(b *testing.B) {
	for _, rows := range []int{1000, 100000} {
		b.Run(strconv.Itoa(rows), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				writer, err := newXLSXReport(io.Discard, 1<<30, time.UTC)
				if err != nil {
					b.Fatal(err)
				}
				cells := []exportCell{{text: "9007199254740993", kind: integerCell}, {text: "12345678901234567890.001", kind: decimalCell}, {text: "2026-09-25T17:30:00Z", kind: dateTimeCell}, {text: "ordinary text"}}
				for range rows {
					if err := writer.Row(cells); err != nil {
						b.Fatal(err)
					}
				}
				if err := writer.Finish(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestXLSXTypedCellsUseExactNumbersAndPresentationZoneDates(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Kuala_Lumpur")
	if err != nil {
		t.Fatal(err)
	}
	cells := []exportCell{
		{text: "42", kind: integerCell},
		{text: "9007199254740993", kind: integerCell},
		{text: "1234567890123456", kind: integerCell},
		{text: "-123456789012345", kind: integerCell},
		{text: "1.25", kind: decimalCell},
		{text: "123456789012345.6", kind: decimalCell},
		{text: "0.000100", kind: decimalCell},
		{text: "2.5", kind: numberCell},
		{text: "2026-09-25", kind: dateCell},
		{text: "2026-09-25T17:30:00Z", kind: dateTimeCell},
		{text: "2026-09-25T06:00:00", kind: localDateTimeCell},
		{text: "1899-01-01", kind: dateCell},
		{text: "", kind: integerCell},
		{text: "not a number", kind: decimalCell},
		{text: "12", kind: textCell},
		{text: "true", kind: booleanCell},
		{text: "false", kind: booleanCell},
	}
	var output bytes.Buffer
	layout := xlsxLayout()
	layout.location = zone
	writer, err := newReportWriter(&output, layout, []string{"Value"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	if err := writer.Row(cells); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	_, sheet := readWorkbook(t, output.Bytes())
	got := sheet.Rows[1].Cells
	type want struct{ typ, style, value, text string }
	wants := []want{
		{"", "1", "42", ""},
		{"inlineStr", "", "", "9007199254740993"},
		{"inlineStr", "", "", "1234567890123456"},
		{"", "1", "-123456789012345", ""},
		{"", "", "1.25", ""},
		{"inlineStr", "", "", "123456789012345.6"},
		{"", "", "0.000100", ""},
		{"", "", "2.5", ""},
		// 2026-09-25 is day 46290; 17:30Z is 01:30 on the 26th in the zone.
		{"", "2", "46290", ""},
		{"", "3", strconv.FormatFloat(46291+1.5/24, 'f', -1, 64), ""},
		{"", "3", "46290.25", ""},
		{"inlineStr", "", "", "1899-01-01"},
		{"inlineStr", "", "", ""},
		{"inlineStr", "", "", "not a number"},
		{"inlineStr", "", "", "12"},
		{"b", "", "1", ""},
		{"b", "", "0", ""},
	}
	for i, cell := range got {
		if cell.Type != wants[i].typ || cell.Style != wants[i].style || cell.Value != wants[i].value || decodeSpreadsheetText(cell.Text) != wants[i].text || cell.Formula != nil {
			t.Fatal("typed cell changed meaning", i, cell)
		}
	}
}

// Identifiers survive an export/import round trip: an integer with more than
// 15 significant digits is written as text, and the importer never rounds.
func TestXLSXSixteenDigitIntegersRoundTripThroughTheImporter(t *testing.T) {
	ids := []string{"1234567890123456", "123456789012345", "-999999999999999", "9007199254740993", "1000000000000000"}
	var output bytes.Buffer
	writer, err := newReportWriter(&output, xlsxLayout(), []string{"ID", "Label"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	for _, id := range ids {
		if err := writer.Row([]exportCell{{text: id, kind: integerCell}, {text: id, kind: textCell}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	_, sheet := readWorkbook(t, output.Bytes())
	for i, want := range []string{"inlineStr", "", "", "inlineStr", "inlineStr"} {
		if sheet.Rows[i+1].Cells[0].Type != want {
			t.Fatal("integer cell precision rule changed", ids[i], sheet.Rows[i+1].Cells[0])
		}
	}
	type imported struct {
		ID    int64
		Label string
	}
	definition := importer.Define(importer.Spec[imported]{Columns: []importer.Column[imported]{
		importer.Field("ID", foundryhttp.IntegerQuery[int64](), func(row *imported, v int64) { row.ID = v }),
		importer.Field("Label", foundryhttp.StringQuery[string](), func(row *imported, v string) { row.Label = v }),
	}})
	var got []imported
	report, err := definition.Run(t.Context(), importer.XLSXSource(bytes.NewReader(output.Bytes()), int64(output.Len())), func(_ context.Context, rows []importer.Row[imported]) error {
		for _, row := range rows {
			got = append(got, row.Value)
		}
		return nil
	})
	if err != nil || report.Failed != 0 || len(got) != len(ids) {
		t.Fatal("exported identifiers did not import", report, err)
	}
	for i, id := range ids {
		if strconv.FormatInt(got[i].ID, 10) != id || got[i].Label != id {
			t.Fatal("identifier changed in the round trip", id, got[i])
		}
	}
}
