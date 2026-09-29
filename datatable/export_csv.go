package datatable

import (
	"encoding/csv"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type reportWriter interface {
	Row([]exportCell) error
	Finish() error
	Abort()
}

// reportLayout is the validated per-export writer configuration.
type reportLayout struct {
	format        ExportFormat
	byteOrderMark bool
	location      *time.Location
	maxXMLBytes   int64
}

func newReportWriter(output io.Writer, layout reportLayout, headers []string) (reportWriter, error) {
	var writer reportWriter
	switch layout.format {
	case CSV:
		if layout.byteOrderMark {
			if _, err := io.WriteString(output, "\uFEFF"); err != nil {
				return nil, err
			}
		}
		writer = &csvReport{writer: csv.NewWriter(output)}
	case XLSX:
		location := layout.location
		if location == nil {
			location = time.UTC
		}
		xlsx, err := newXLSXReport(output, layout.maxXMLBytes, location)
		if err != nil {
			return nil, err
		}
		writer = xlsx
	default:
		return nil, invalid("invalid report format")
	}
	heading := make([]exportCell, len(headers))
	for i, text := range headers {
		heading[i] = exportCell{text: text}
	}
	if err := writer.Row(heading); err != nil {
		writer.Abort()
		return nil, err
	}
	return writer, nil
}

type csvReport struct {
	writer *csv.Writer
	record []string
}

// Row writes cell text, including control characters, which CSV can carry
// literally. Typed spreadsheet values apply only to XLSX.
func (w *csvReport) Row(cells []exportCell) error {
	w.record = w.record[:0]
	for _, cell := range cells {
		w.record = append(w.record, csvLiteral(cell.text))
	}
	return w.writer.Write(w.record)
}
func (w *csvReport) Finish() error { w.writer.Flush(); return w.writer.Error() }
func (w *csvReport) Abort()        {}

// CSV is text, not a typed spreadsheet format. Prefix values that spreadsheet
// programs may interpret as formulas (also after leading whitespace) with an
// apostrophe, then let encoding/csv quote delimiters/newlines/quotes. Users who
// re-save/re-import CSV must reapply their spreadsheet's safe import policy.
func csvLiteral(text string) string {
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + text
	}
	if strings.HasPrefix(text, "\t") || strings.HasPrefix(text, "\r") {
		return "'" + text
	}
	return text
}

// truncationMarker ends a cell shortened to its byte or spreadsheet bound.
const truncationMarker = "…[truncated]"

// minCellBytes keeps room for the truncation marker and a useful prefix.
const minCellBytes = 64

// exportText makes one cell writable instead of failing a whole export:
// invalid UTF-8 becomes U+FFFD, and text above maxBytes or maxUnits UTF-16
// code units (the spreadsheet cell limit) is cut at a character boundary and
// ends with truncationMarker. Control characters are left to the writer.
func exportText(text string, maxBytes, maxUnits int) string {
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "\uFFFD")
	}
	// Every rune has at least as many UTF-8 bytes as UTF-16 units.
	if len(text) <= maxBytes && len(text) <= maxUnits {
		return text
	}
	units := 0
	for _, r := range text {
		units += utf16Units(r)
	}
	if len(text) <= maxBytes && units <= maxUnits {
		return text
	}
	budgetBytes := maxBytes - len(truncationMarker)
	budgetUnits := maxUnits - utf8.RuneCountInString(truncationMarker)
	end, units := 0, 0
	for i, r := range text {
		size := utf8.RuneLen(r)
		if i+size > budgetBytes || units+utf16Units(r) > budgetUnits {
			break
		}
		end, units = i+size, units+utf16Units(r)
	}
	return text[:end] + truncationMarker
}

func utf16Units(r rune) int {
	if r > 0xffff {
		return 2
	}
	return 1
}
