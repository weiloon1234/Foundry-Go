package datatable

import (
	"archive/zip"
	"bytes"
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
)

type sheetCell struct {
	Reference string  `xml:"r,attr"`
	Type      string  `xml:"t,attr"`
	Formula   *string `xml:"f"`
	Text      string  `xml:"is>t"`
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
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml"} {
		if parts[name] == nil {
			t.Fatal("missing workbook part", name)
		}
	}
	if len(parts) != 5 {
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

func TestXLSXStructureLiteralCellsAndExactValues(t *testing.T) {
	input := []string{"=SUM(A1:A2)", "+1", "-2", "@link", "9007199254740993", "12345678901234567890.001", "a<&>\"b", "\t padded \n", "line\rnext", "_x000D_", "_x005F_x0041_", "中文😀", ""}
	var output bytes.Buffer
	writer, err := newReportWriter(&output, XLSX, DefaultConfig(), []string{"Name", "=heading"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	if err := writer.Row(input); err != nil {
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
	for name, want := range map[string]string{"_rels/.rels": "xl/workbook.xml", "xl/_rels/workbook.xml.rels": "worksheets/sheet1.xml"} {
		relationships.Items = nil
		if err := xml.Unmarshal(parts[name], &relationships); err != nil || len(relationships.Items) != 1 {
			t.Fatal("invalid workbook relationship", err)
		}
		item := relationships.Items[0]
		if item.ID != "rId1" || item.Target != want || item.Mode != "" {
			t.Fatal("external or broken relationship", item)
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
	again, err := newReportWriter(&repeat, XLSX, DefaultConfig(), []string{"Name", "=heading"})
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Row(input); err != nil {
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
	if _, err := newXLSXReport(io.Discard, 1); err == nil {
		t.Fatal("metadata escaped the XML budget")
	}
	writer, err := newXLSXReport(io.Discard, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	writer.row = 1_048_575
	if err := writer.Row([]string{"last"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(nil); err == nil {
		t.Fatal("worksheet row limit ignored")
	}
	writer.row = 0
	if err := writer.Row(make([]string, 16385)); err == nil {
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
	broken, err := newXLSXReport(output, 1<<20)
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
	writer, err := newXLSXReport(sink, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	initial := sink.bytes
	for row := range 4096 {
		digest := sha256.Sum256([]byte(strconv.Itoa(row)))
		if err := writer.Row([]string{hex.EncodeToString(digest[:]), strings.Repeat("x", 256)}); err != nil {
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
		if len(text) > 4096 || validateCell(text, 4096) != nil {
			t.Skip()
		}
		var output bytes.Buffer
		writer, err := newReportWriter(&output, XLSX, DefaultConfig(), []string{"Value"})
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Abort()
		if err := writer.Row([]string{text}); err != nil {
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
	writer, err := newXLSXReport(io.Discard, 256<<20)
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
		if err := writer.Row([]string{strconv.Itoa(row) + ":" + padding}); err != nil {
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
				writer, err := newXLSXReport(io.Discard, 1<<30)
				if err != nil {
					b.Fatal(err)
				}
				for range rows {
					if err := writer.Row([]string{"9007199254740993", "12345678901234567890.001", "ordinary text"}); err != nil {
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
