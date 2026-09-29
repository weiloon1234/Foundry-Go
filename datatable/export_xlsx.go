package datatable

import (
	"archive/zip"
	"bufio"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable/internal/spreadsheet"
)

const (
	maxWorksheetRows     = spreadsheet.MaxRows
	maxWorksheetColumns  = spreadsheet.MaxColumns
	maxCellUTF16Units    = spreadsheet.MaxCellUTF16Units
	xlsxSheetBufferBytes = 32 << 10
)

// cellKind selects how XLSX stores a scalar cell. Text is always safe; typed
// kinds become numbers, booleans or dates only when the codec text is exactly
// representable.
type cellKind = spreadsheet.Kind

const (
	textCell          = spreadsheet.Text
	integerCell       = spreadsheet.Integer
	numberCell        = spreadsheet.Number
	decimalCell       = spreadsheet.Decimal
	booleanCell       = spreadsheet.Boolean
	dateCell          = spreadsheet.Date
	dateTimeCell      = spreadsheet.DateTime
	localDateTimeCell = spreadsheet.LocalDateTime
)

type exportCell struct {
	text string
	kind cellKind
}

func scalarCellKind(typ contract.Type) cellKind { return spreadsheet.KindOf(typ) }

// Cell styles declared by xlsxStyles, by cellXfs index.
const (
	generalStyle  = 0
	integerStyle  = 1
	dateStyle     = 2
	dateTimeStyle = 3
)

// This focused OOXML writer emits one worksheet of inline-string, number,
// boolean and date cells. It has no shared-string table or workbook-sized buffer.
// Integers and decimals beyond 15 significant digits and unrepresentable dates
// stay exact text; no cell is a formula, hyperlink, macro or external link.
// ZIP central-directory memory is constant because there are always six parts.
type xlsxReport struct {
	archive  *zip.Writer
	buffered *bufio.Writer
	sheet    io.Writer
	location *time.Location
	line     []byte
	row      int
	closed   bool
}
type xmlBudget struct {
	writer    io.Writer
	remaining int64
}

func (w *xmlBudget) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, invalid("export XML byte limit exceeded")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

const xlsxStyles = `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd hh:mm:ss"/></numFmts><fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func newXLSXReport(output io.Writer, maximum int64, location *time.Location) (*xlsxReport, error) {
	archive := zip.NewWriter(output)
	complete := false
	defer func() {
		if !complete {
			_ = archive.Close()
		}
	}()
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Report" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", xlsxStyles},
	}
	for _, part := range parts {
		if int64(len(part.body)) > maximum {
			return nil, invalid("export XML byte limit exceeded")
		}
		maximum -= int64(len(part.body))
		writer, err := xlsxPart(archive, part.name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(writer, part.body); err != nil {
			return nil, err
		}
	}
	sheet, err := xlsxPart(archive, "xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	// Buffer the many small row writes in front of the compressor; the
	// budget still counts every uncompressed byte before it is buffered.
	buffered := bufio.NewWriterSize(sheet, xlsxSheetBufferBytes)
	bounded := &xmlBudget{writer: buffered, remaining: maximum}
	if _, err := io.WriteString(bounded, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return nil, err
	}
	complete = true
	return &xlsxReport{archive: archive, buffered: buffered, sheet: bounded, location: location}, nil
}
func xlsxPart(archive *zip.Writer, name string) (io.Writer, error) {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	return archive.CreateHeader(header)
}

// Row encodes the complete row into one reused buffer, then writes it once.
func (w *xlsxReport) Row(cells []exportCell) error {
	if w.closed {
		return invalid("XLSX writer is closed")
	}
	if w.row >= maxWorksheetRows || len(cells) > maxWorksheetColumns {
		return invalid("XLSX dimensions exceed their limits")
	}
	w.row++
	line := append(w.line[:0], `<row r="`...)
	line = strconv.AppendInt(line, int64(w.row), 10)
	line = append(line, `">`...)
	for i, cell := range cells {
		line = append(line, `<c r="`...)
		line = spreadsheet.AppendColumn(line, i+1)
		line = strconv.AppendInt(line, int64(w.row), 10)
		if cell.kind == booleanCell && (cell.text == "true" || cell.text == "false") {
			line = append(line, `" t="b"><v>`...)
			line = append(line, "01"[boolIndex(cell.text == "true")])
			line = append(line, `</v></c>`...)
			continue
		}
		if number, style, ok := spreadsheetValue(cell, w.location); ok {
			if style != generalStyle {
				line = append(line, `" s="`...)
				line = strconv.AppendInt(line, int64(style), 10)
			}
			line = append(line, `"><v>`...)
			line = append(line, number...)
			line = append(line, `</v></c>`...)
			continue
		}
		line = append(line, `" t="inlineStr"><is><t xml:space="preserve">`...)
		line = spreadsheet.AppendText(line, cell.text)
		line = append(line, `</t></is></c>`...)
	}
	line = append(line, `</row>`...)
	w.line = line
	_, err := w.sheet.Write(line)
	return err
}
func (w *xlsxReport) Finish() error {
	if w.closed {
		return invalid("XLSX writer is closed")
	}
	w.closed = true
	_, err := io.WriteString(w.sheet, `</sheetData></worksheet>`)
	if err == nil {
		err = w.buffered.Flush()
	}
	return errors.Join(err, w.archive.Close())
}
func (w *xlsxReport) Abort() {
	if !w.closed {
		w.closed = true
		_ = w.archive.Close()
	}
}
func xlsxColumn(n int) string { return string(spreadsheet.AppendColumn(nil, n)) }
func boolIndex(v bool) int {
	if v {
		return 1
	}
	return 0
}

// spreadsheetValue returns the numeric <v> text and style of an exactly
// representable typed cell. ok is false for text and for values a spreadsheet
// would round or cannot date, which then remain exact inline text.
func spreadsheetValue(cell exportCell, location *time.Location) (string, int, bool) {
	switch cell.kind {
	case integerCell:
		// Like decimals, integers beyond 15 significant digits (such as 16-digit
		// identifiers) stay text: a spreadsheet would round the stored number.
		if _, err := strconv.ParseInt(cell.text, 10, 64); err != nil || !exactSpreadsheetDecimal(cell.text) {
			return "", 0, false
		}
		return cell.text, integerStyle, true
	case numberCell:
		f, err := strconv.ParseFloat(cell.text, 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return "", 0, false
		}
		return strconv.FormatFloat(f, 'g', -1, 64), generalStyle, true
	case decimalCell:
		if !exactSpreadsheetDecimal(cell.text) {
			return "", 0, false
		}
		return cell.text, generalStyle, true
	case dateCell:
		day, err := time.Parse(time.DateOnly, cell.text)
		if err != nil {
			return "", 0, false
		}
		return serialCell(day, dateStyle)
	case dateTimeCell:
		instant, err := time.Parse(time.RFC3339Nano, cell.text)
		if err != nil {
			return "", 0, false
		}
		// Spreadsheets have no zone: write the presentation zone's wall time.
		return serialCell(instant.In(location), dateTimeStyle)
	case localDateTimeCell:
		wall, err := time.Parse(spreadsheet.LocalDateTimeLayout, cell.text)
		if err != nil {
			return "", 0, false
		}
		return serialCell(wall, dateTimeStyle)
	}
	return "", 0, false
}

func serialCell(wall time.Time, style int) (string, int, bool) {
	serial, ok := spreadsheet.Serial(wall)
	return serial, style, ok
}

// exactSpreadsheetDecimal accepts canonical decimal text with at most 15
// significant digits, which a spreadsheet stores and displays without change.
func exactSpreadsheetDecimal(text string) bool {
	digits, leading, seenPoint, trailingZeros := 0, true, false, 0
	body := strings.TrimPrefix(text, "-")
	if body == "" || body[0] == '.' || body[len(body)-1] == '.' {
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '.' && !seenPoint:
			seenPoint = true
		case c >= '0' && c <= '9':
			if c == '0' && leading {
				continue
			}
			leading = false
			digits++
			if c == '0' && seenPoint {
				trailingZeros++
			} else {
				trailingZeros = 0
			}
		default:
			return false
		}
	}
	return digits-trailingZeros <= spreadsheet.ExactDigits
}
