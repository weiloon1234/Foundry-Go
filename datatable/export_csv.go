package datatable

import (
	"encoding/csv"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

type reportWriter interface {
	Row([]string) error
	Finish() error
	Abort()
}

func newReportWriter(output io.Writer, format ExportFormat, config Config, headers []string) (reportWriter, error) {
	var writer reportWriter
	switch format {
	case CSV:
		writer = &csvReport{writer: csv.NewWriter(output)}
	case XLSX:
		xlsx, err := newXLSXReport(output, config.MaxXMLBytes)
		if err != nil {
			return nil, err
		}
		writer = xlsx
	default:
		return nil, invalid("invalid report format")
	}
	if err := writer.Row(headers); err != nil {
		writer.Abort()
		return nil, err
	}
	return writer, nil
}

type csvReport struct{ writer *csv.Writer }

func (w *csvReport) Row(cells []string) error {
	safe := make([]string, len(cells))
	for i, text := range cells {
		safe[i] = csvLiteral(text)
	}
	return w.writer.Write(safe)
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
func validateCell(text string, maximum int) error {
	if len(text) > maximum || !utf8.ValidString(text) {
		return invalid("export cell exceeds its text bound")
	}
	units := 0
	for _, r := range text {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' || r == 0xfffe || r == 0xffff {
			return invalid("export cell contains unsupported control characters")
		}
		units++
		if r > 0xffff {
			units++
		}
		if units > maxCellUTF16Units {
			return invalid("export cell exceeds spreadsheet character limit")
		}
	}
	return nil
}
