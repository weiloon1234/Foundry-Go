package datatable

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// textCells converts plain strings to untyped export cells.
func textCells(texts ...string) []exportCell {
	cells := make([]exportCell, len(texts))
	for i, text := range texts {
		cells[i] = exportCell{text: text}
	}
	return cells
}

func csvLayout() reportLayout {
	return reportLayout{format: CSV, maxXMLBytes: DefaultConfig().MaxXMLBytes}
}

func TestCSVQuotesTextAndNeutralizesSpreadsheetFormulas(t *testing.T) {
	input := []string{"plain", "=1+1", " +SUM(A1:A2)", "-42", "@lookup", "\tvalue", "\rvalue", "\u2003=1", "a,b", "say \"hello\"", "line\nnext", "中文", "", "bell\x07\x00\x1b", "\ufffe"}
	want := []string{"plain", "'=1+1", "' +SUM(A1:A2)", "'-42", "'@lookup", "'\tvalue", "'\rvalue", "'\u2003=1", "a,b", "say \"hello\"", "line\nnext", "中文", "", "bell\x07\x00\x1b", "\ufffe"}
	var output bytes.Buffer
	writer, err := newReportWriter(&output, csvLayout(), []string{"=unsafe heading", "Safe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(textCells(input...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(bytes.NewReader(output.Bytes()))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || !reflect.DeepEqual(rows, [][]string{{"'=unsafe heading", "Safe"}, want}) {
		t.Fatal("CSV lost quoting or formula neutralization", rows, err)
	}
}

type failureWriter struct {
	err   error
	calls int
}

func (w *failureWriter) Write([]byte) (int, error) { w.calls++; return 0, w.err }

func TestCSVFinalizationReportsBufferedWriteFailure(t *testing.T) {
	stop := errors.New("disk write failed")
	output := &failureWriter{err: stop}
	writer, err := newReportWriter(output, csvLayout(), []string{"Name"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(textCells("buffered")); err != nil {
		t.Fatal(err)
	}
	if output.calls != 0 {
		t.Fatal("small fixture did not exercise buffered finalization")
	}
	if err := writer.Finish(); !errors.Is(err, stop) {
		t.Fatal("flush failure was hidden", err)
	}
	writer.Abort()
	if _, err := newReportWriter(io.Discard, reportLayout{format: "other"}, nil); err == nil {
		t.Fatal("unknown export format accepted")
	}
}

func TestExportTextTruncatesOversizedCellsWithAnExplicitMarker(t *testing.T) {
	for _, text := range []string{"", "\t\n\r\x00\x1f\ufffe", "中文😀", strings.Repeat("a", 32767), strings.Repeat("😀", 16383) + "a"} {
		if got := exportText(text, 128<<10, maxCellUTF16Units); got != text {
			t.Fatal("valid spreadsheet text changed", len(text))
		}
	}
	if got := exportText(string([]byte{'a', 0xff, 'b'}), 64, maxCellUTF16Units); got != "a\uFFFDb" {
		t.Fatal("invalid UTF-8 was not replaced", got)
	}
	units := func(text string) int {
		n := 0
		for _, r := range text {
			n += utf16Units(r)
		}
		return n
	}
	for _, text := range []string{strings.Repeat("a", 32768), strings.Repeat("😀", 16384), strings.Repeat("a", 32766) + "😀"} {
		got := exportText(text, 128<<10, maxCellUTF16Units)
		if !strings.HasSuffix(got, truncationMarker) || units(got) > maxCellUTF16Units || !strings.HasPrefix(text, strings.TrimSuffix(got, truncationMarker)) {
			t.Fatal("spreadsheet cell limit was not truncated safely", units(got))
		}
	}
	got := exportText(strings.Repeat("中", 100), 64, math.MaxInt)
	if len(got) > 64 || !strings.HasSuffix(got, truncationMarker) || !utf8.ValidString(got) {
		t.Fatal("cell byte budget was not truncated at a character boundary", len(got))
	}
	if exportText(strings.Repeat("a", 64), 64, math.MaxInt) != strings.Repeat("a", 64) {
		t.Fatal("text at the byte bound was truncated")
	}
}

func TestCSVByteOrderMarkPrefixesTheCompleteFile(t *testing.T) {
	var output bytes.Buffer
	layout := csvLayout()
	layout.byteOrderMark = true
	writer, err := newReportWriter(&output, layout, []string{"Name"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(textCells("Zoë")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\uFEFFName\nZoë\n" {
		t.Fatal("CSV byte-order mark missing or duplicated", output.String())
	}
}
