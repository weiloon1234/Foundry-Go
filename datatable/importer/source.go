package importer

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/datatable/internal/spreadsheet"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Format selects the container of an uploaded table.
type Format string

const (
	CSV  Format = "csv"
	XLSX Format = "xlsx"
)

// Source is one uploaded table. The caller owns the underlying reader and
// closes it after Run returns. Construct it with CSVSource or XLSXSource.
type Source struct {
	format Format
	reader io.Reader
	at     io.ReaderAt
	size   int64
	comma  rune
	sheet  string
	err    error
}

// CSVSource reads comma-separated UTF-8 text (RFC 4180 quoting). A leading
// byte-order mark is ignored.
func CSVSource(reader io.Reader) Source {
	return Source{format: CSV, reader: reader, comma: ','}
}

// WithComma selects another CSV field delimiter, such as ';' or '\t'.
func (s Source) WithComma(comma rune) Source {
	if s.format != CSV || comma == '"' || comma == '\r' || comma == '\n' || comma == 0xfffd || comma < 0x20 && comma != '\t' {
		s.err = invalid("invalid CSV delimiter")
	}
	s.comma = comma
	return s
}

// XLSXSource reads the first worksheet of an Office Open XML workbook. The
// archive is random access, so the caller supplies its exact size.
func XLSXSource(reader io.ReaderAt, size int64) Source {
	return Source{format: XLSX, at: reader, size: size}
}

// WithSheet selects a worksheet by its exact name instead of the first one.
func (s Source) WithSheet(name string) Source {
	if s.format != XLSX || name == "" || len(name) > 128 {
		s.err = invalid("invalid worksheet selection")
	}
	s.sheet = name
	return s
}

func (s Source) validate() error {
	if s.err != nil {
		return s.err
	}
	switch {
	case s.format == CSV && s.reader != nil:
		return nil
	case s.format == XLSX && s.at != nil && s.size >= 0:
		return nil
	}
	return invalid("import source is not defined")
}

// rawCell is one source cell before conversion to a declared column's text.
type rawCell struct {
	text string
	kind rawKind
}
type rawKind uint8

const (
	rawText rawKind = iota
	rawNumber
	rawBoolean
	rawInvalid
)

// rowReader yields physical rows with their 1-based row numbers in increasing
// order, then io.EOF. The returned cells are owned by the caller.
type rowReader interface {
	next() (int, []rawCell, error)
	close()
}

func (s Source) open(ctx context.Context, limits Limits) (rowReader, error) {
	if s.format == CSV {
		return openCSV(ctx, s, limits), nil
	}
	return openXLSX(ctx, s, limits)
}

type csvReader struct {
	reader  *csv.Reader
	started bool
	columns int
}

func openCSV(ctx context.Context, source Source, limits Limits) *csvReader {
	guarded := &csvGuard{ctx: ctx, reader: source.reader, remaining: limits.MaxInputBytes, maxRecord: int64(limits.MaxRowBytes)}
	reader := csv.NewReader(guarded)
	reader.Comma = source.comma
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	return &csvReader{reader: reader, columns: limits.MaxColumns}
}
func (r *csvReader) next() (int, []rawCell, error) {
	record, err := r.reader.Read()
	if err != nil {
		var malformed *csv.ParseError
		if errors.As(err, &malformed) && !errors.Is(err, fault.Invalid) {
			return 0, nil, invalid("import CSV is malformed")
		}
		return 0, nil, err
	}
	if len(record) > r.columns {
		return 0, nil, invalid("import row exceeds its column limit")
	}
	// Report the line on which the record starts: blank lines count, as they
	// do when a spreadsheet opens the file, and a quoted line break does not.
	number, _ := r.reader.FieldPos(0)
	cells := make([]rawCell, len(record))
	for i, text := range record {
		if i == 0 && !r.started {
			text = trimByteOrderMark(text)
		}
		cells[i] = rawCell{text: text}
	}
	r.started = true
	return number, cells, nil
}
func (*csvReader) close() {}

func trimByteOrderMark(text string) string {
	const mark = "\xef\xbb\xbf"
	if len(text) >= len(mark) && text[:len(mark)] == mark {
		return text[len(mark):]
	}
	return text
}

// csvGuard bounds total input and one record's bytes. It tracks RFC 4180
// quoting so a quoted line break does not end the record being measured.
type csvGuard struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
	maxRecord int64
	record    int64
	quoted    bool
}

func (g *csvGuard) Read(p []byte) (int, error) {
	if err := g.ctx.Err(); err != nil {
		return 0, err
	}
	if g.remaining <= 0 {
		// Probe for one more byte: exactly MaxInputBytes of input is valid.
		var probe [1]byte
		if n, err := g.reader.Read(probe[:]); n > 0 {
			return 0, invalid("import input exceeds its byte limit")
		} else if err != nil {
			return 0, err
		}
		return 0, nil
	}
	if int64(len(p)) > g.remaining {
		p = p[:g.remaining]
	}
	n, err := g.reader.Read(p)
	if n < 0 || n > len(p) {
		return 0, fault.New(fault.Internal, "import reader returned an invalid count")
	}
	g.remaining -= int64(n)
	for _, b := range p[:n] {
		switch {
		case b == '"':
			g.quoted = !g.quoted
		case b == '\n' && !g.quoted:
			g.record = 0
			continue
		}
		if g.record++; g.record > g.maxRecord {
			return 0, invalid("import row exceeds its byte limit")
		}
	}
	return n, err
}

// convert produces the codec text of one cell for a column kind. A number
// reaches a floating-point column exactly as stored. Other columns, including
// text, accept only numbers a spreadsheet shows without rounding (at most 15
// significant digits); anything else, such as a 16-digit identifier stored as a
// number, is a type issue instead of a silently rounded value. Numbers in date
// columns are serial dates, interpreted in location.
func convert(cell rawCell, kind spreadsheet.Kind, location *time.Location) (string, bool) {
	switch cell.kind {
	case rawText:
		return cell.text, true
	case rawBoolean:
		switch cell.text {
		case "1":
			return "true", true
		case "0":
			return "false", true
		}
		return "", false
	case rawNumber:
		if cell.text == "" {
			return "", true
		}
	default:
		return "", false
	}
	switch kind {
	case spreadsheet.Date, spreadsheet.DateTime, spreadsheet.LocalDateTime:
		serial, err := strconv.ParseFloat(cell.text, 64)
		if err != nil {
			return "", false
		}
		wall, ok := spreadsheet.Time(serial)
		if !ok {
			return "", false
		}
		switch kind {
		case spreadsheet.Date:
			if wall.Hour() != 0 || wall.Minute() != 0 || wall.Second() != 0 || wall.Nanosecond() != 0 {
				return "", false
			}
			return wall.Format(time.DateOnly), true
		case spreadsheet.DateTime:
			local := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), location)
			return local.UTC().Format(time.RFC3339Nano), true
		}
		return wall.Format(spreadsheet.LocalDateTimeLayout), true
	case spreadsheet.Boolean:
		switch cell.text {
		case "1":
			return "true", true
		case "0":
			return "false", true
		}
		return "", false
	}
	if kind == spreadsheet.Number {
		return spreadsheet.ExactNumber(cell.text)
	}
	return spreadsheet.DisplayNumber(cell.text)
}
