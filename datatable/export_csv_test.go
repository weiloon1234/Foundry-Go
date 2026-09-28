package datatable

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestCSVQuotesTextAndNeutralizesSpreadsheetFormulas(t *testing.T) {
	input := []string{"plain", "=1+1", " +SUM(A1:A2)", "-42", "@lookup", "\tvalue", "\rvalue", "\u2003=1", "a,b", "say \"hello\"", "line\nnext", "中文", ""}
	want := []string{"plain", "'=1+1", "' +SUM(A1:A2)", "'-42", "'@lookup", "'\tvalue", "'\rvalue", "'\u2003=1", "a,b", "say \"hello\"", "line\nnext", "中文", ""}
	var output bytes.Buffer
	writer, err := newReportWriter(&output, CSV, DefaultConfig(), []string{"=unsafe heading", "Safe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Row(input); err != nil {
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
	writer, err := newReportWriter(output, CSV, DefaultConfig(), []string{"Name"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Row([]string{"buffered"}); err != nil {
		t.Fatal(err)
	}
	if output.calls != 0 {
		t.Fatal("small fixture did not exercise buffered finalization")
	}
	if err := writer.Finish(); !errors.Is(err, stop) {
		t.Fatal("flush failure was hidden", err)
	}
	writer.Abort()
	if _, err := newReportWriter(io.Discard, ExportFormat("other"), DefaultConfig(), nil); err == nil {
		t.Fatal("unknown export format accepted")
	}
}

func TestExportCellBoundsUseBytesUTF16AndXMLCharacters(t *testing.T) {
	for _, text := range []string{"", "\t\n\r", "中文😀", strings.Repeat("a", 32767), strings.Repeat("😀", 16383) + "a"} {
		if err := validateCell(text, 128<<10); err != nil {
			t.Fatal("valid spreadsheet text rejected", len(text), err)
		}
	}
	for _, text := range []string{"\x00", "\x01", "\uFFFE", "\uFFFF", string([]byte{0xff}), strings.Repeat("a", 32768), strings.Repeat("😀", 16384)} {
		if validateCell(text, 128<<10) == nil {
			t.Fatal("invalid or oversized cell accepted", len(text))
		}
	}
	if validateCell("中文", 5) == nil {
		t.Fatal("cell byte budget counted runes instead of bytes")
	}
}
