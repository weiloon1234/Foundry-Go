package datatable

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	maxWorksheetRows    = 1_048_576
	maxWorksheetColumns = 16_384
	maxCellUTF16Units   = 32_767
)

// This focused OOXML writer emits one worksheet of plain inline-string cells.
// It has no shared-string table or workbook-sized buffer. Exact integers and
// decimals stay text; no cell is a formula, hyperlink, macro or external link.
// ZIP central-directory memory is constant because there are always five parts.
type xlsxReport struct {
	archive *zip.Writer
	sheet   io.Writer
	row     int
	closed  bool
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
func newXLSXReport(output io.Writer, maximum int64) (*xlsxReport, error) {
	archive := zip.NewWriter(output)
	complete := false
	defer func() {
		if !complete {
			_ = archive.Close()
		}
	}()
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Report" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
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
	bounded := &xmlBudget{writer: sheet, remaining: maximum}
	if _, err := io.WriteString(bounded, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return nil, err
	}
	complete = true
	return &xlsxReport{archive: archive, sheet: bounded}, nil
}
func xlsxPart(archive *zip.Writer, name string) (io.Writer, error) {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	return archive.CreateHeader(header)
}
func (w *xlsxReport) Row(cells []string) error {
	if w.closed {
		return invalid("XLSX writer is closed")
	}
	if w.row >= maxWorksheetRows || len(cells) > maxWorksheetColumns {
		return invalid("XLSX dimensions exceed their limits")
	}
	w.row++
	if _, err := fmt.Fprintf(w.sheet, `<row r="%d">`, w.row); err != nil {
		return err
	}
	for i, text := range cells {
		if _, err := fmt.Fprintf(w.sheet, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">`, xlsxColumn(i+1), w.row); err != nil {
			return err
		}
		if err := xml.EscapeText(w.sheet, []byte(ooxmlLiteral(text))); err != nil {
			return err
		}
		if _, err := io.WriteString(w.sheet, `</t></is></c>`); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w.sheet, `</row>`)
	return err
}
func (w *xlsxReport) Finish() error {
	if w.closed {
		return invalid("XLSX writer is closed")
	}
	w.closed = true
	_, err := io.WriteString(w.sheet, `</sheetData></worksheet>`)
	return errors.Join(err, w.archive.Close())
}
func (w *xlsxReport) Abort() {
	if !w.closed {
		w.closed = true
		_ = w.archive.Close()
	}
}
func xlsxColumn(n int) string {
	var buffer [4]byte
	i := len(buffer)
	for n > 0 {
		n--
		i--
		buffer[i] = byte('A' + n%26)
		n /= 26
	}
	return string(buffer[i:])
}
func ooxmlLiteral(text string) string {
	var result strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' {
			result.WriteString("_x000D_")
			continue
		}
		// Protect every literal escape prefix, including incomplete sequences.
		// Expanding a later CR inserts an underscore and could otherwise finish
		// a new escape, for example "_x0000\r" becoming "_x0000_x000D_".
		if text[i] == '_' && i+1 < len(text) && text[i+1] == 'x' {
			result.WriteString("_x005F_")
			continue
		}
		result.WriteByte(text[i])
	}
	return result.String()
}
