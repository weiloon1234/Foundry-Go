// Package importer streams CSV and XLSX uploads into typed rows. A heading row
// maps declared columns onto a row struct through scalar codecs; each row is
// then checked by ordinary validation rules. Valid rows reach the application
// handler in bounded chunks while invalid rows are reported with their row
// number and issues. Input, row, cell, decompressed XML and shared-string
// sizes are all bounded; no complete file or result set is held in memory.
package importer

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable/internal/spreadsheet"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonpointer"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Limits bound one import. DefaultLimits is the source of the defaults.
type Limits struct {
	// MaxRows bounds data rows after the heading; exceeding it fails the import.
	MaxRows int
	// MaxColumns bounds the columns of any row.
	MaxColumns int
	// MaxCellBytes bounds one cell; a longer cell is a row issue.
	MaxCellBytes int
	// MaxRowBytes bounds one CSV record, including quoted line breaks.
	// Longer records fail the import: they cannot be parsed incrementally.
	MaxRowBytes int
	// MaxInputBytes bounds CSV bytes read or the XLSX archive size.
	MaxInputBytes int64
	// MaxXMLBytes bounds all decompressed XLSX parts that are read. No single
	// XML token may exceed eight times MaxCellBytes plus 4 KiB, whatever this
	// total allows. Raise it together with MaxRows for very wide sheets.
	MaxXMLBytes int64
	// MaxSharedStrings and MaxSharedStringBytes bound the XLSX string table,
	// the only part that must be held in memory for random access.
	MaxSharedStrings     int
	MaxSharedStringBytes int64
	// MaxFailures bounds retained row failures; later failures are counted.
	MaxFailures int
	// ChunkSize is the number of valid rows passed to each handler call.
	ChunkSize  int
	Validation validation.Limits
}

func DefaultLimits() Limits {
	return Limits{
		MaxRows: 100_000, MaxColumns: 1024, MaxCellBytes: 32 << 10, MaxRowBytes: 1 << 20,
		MaxInputBytes: 64 << 20, MaxXMLBytes: 128 << 20,
		MaxSharedStrings: 1_000_000, MaxSharedStringBytes: 64 << 20,
		MaxFailures: 1000, ChunkSize: 500, Validation: validation.DefaultLimits(),
	}
}
func (l Limits) Validate() error {
	if l.MaxRows < 1 || l.MaxRows >= spreadsheet.MaxRows || l.MaxColumns < 1 || l.MaxColumns > spreadsheet.MaxColumns || l.MaxCellBytes < 1 || l.MaxCellBytes > 1<<20 || l.MaxRowBytes < l.MaxCellBytes || l.MaxRowBytes > 64<<20 {
		return invalid("invalid import row limits")
	}
	if l.MaxInputBytes < 1 || l.MaxInputBytes > 1<<30 || l.MaxXMLBytes < 1 || l.MaxXMLBytes > 4<<30 || l.MaxSharedStrings < 0 || l.MaxSharedStrings > 16_000_000 || l.MaxSharedStringBytes < 0 || l.MaxSharedStringBytes > 1<<30 {
		return invalid("invalid import input limits")
	}
	if l.MaxFailures < 0 || l.MaxFailures > 1_000_000 || l.ChunkSize < 1 || l.ChunkSize > 100_000 {
		return invalid("invalid import reporting limits")
	}
	return l.Validation.Validate()
}

// Column maps one file heading onto a field of row type R. Construct it with
// Field or NullableField. Headings match after trimming spaces, ignoring case.
type Column[R any] struct {
	heading  string
	required bool
	kind     spreadsheet.Kind
	assign   func(*R, string) error
	err      error
}

// Field declares a required heading and a non-empty cell parsed by codec. An
// empty cell is a required issue. assign stores the parsed value in the row.
func Field[R, V any](heading string, codec foundryhttp.QueryCodec[V], assign func(*R, V)) Column[R] {
	column := define[R](heading, codec, assign == nil)
	column.required = true
	column.assign = func(row *R, text string) error {
		v, err := codec.Parse(text)
		if err != nil {
			return err
		}
		assign(row, v)
		return nil
	}
	return column
}

// NullableField declares an optional heading. An empty cell, or a file without
// the heading, assigns value.Null; other cells are parsed by codec.
func NullableField[R, V any](heading string, codec foundryhttp.QueryCodec[V], assign func(*R, value.Nullable[V])) Column[R] {
	column := define[R](heading, codec, assign == nil)
	column.assign = func(row *R, text string) error {
		if text == "" {
			assign(row, value.Null[V]())
			return nil
		}
		v, err := codec.Parse(text)
		if err != nil {
			return err
		}
		assign(row, value.Of(v))
		return nil
	}
	return column
}

func define[R, V any](heading string, codec foundryhttp.QueryCodec[V], missing bool) Column[R] {
	column := Column[R]{heading: strings.TrimSpace(heading)}
	info, err := foundryhttp.DescribePathCodec(codec)
	switch {
	case err != nil || missing:
		column.err = invalid("import column requires a described codec and an assignment")
	case column.heading == "" || len(column.heading) > 256 || !utf8.ValidString(column.heading):
		column.err = invalid("invalid import column heading")
	default:
		column.kind = spreadsheet.KindOf(info.Value)
	}
	return column
}

// Spec declares one import. Rules run on each completely parsed row. An empty
// TimeZone means UTC; it converts spreadsheet date-time cells, which have no
// zone, into instants. HeadingRow is 1-based; zero means the first row. Zero
// Limits means DefaultLimits.
type Spec[R any] struct {
	Columns    []Column[R]
	Rules      []validation.Rule[R]
	HeadingRow int
	TimeZone   temporal.ZoneName
	Limits     Limits
}

// Definition is an immutable, validated import declaration.
type Definition[R any] struct{ definition *definition[R] }
type definition[R any] struct {
	columns  []Column[R]
	rule     validation.Rule[R]
	rules    bool
	heading  int
	location *time.Location
	limits   Limits
	err      error
}

func Define[R any](spec Spec[R]) Definition[R] {
	d := &definition[R]{columns: slices.Clone(spec.Columns), heading: spec.HeadingRow, limits: spec.Limits}
	if d.heading == 0 {
		d.heading = 1
	}
	if d.limits == (Limits{}) {
		d.limits = DefaultLimits()
	}
	d.err = d.initialize(spec)
	return Definition[R]{definition: d}
}
func (d *definition[R]) initialize(spec Spec[R]) error {
	if len(d.columns) == 0 || len(d.columns) > spreadsheet.MaxColumns || d.heading < 1 || d.heading >= spreadsheet.MaxRows {
		return invalid("invalid import declaration")
	}
	if err := d.limits.Validate(); err != nil {
		return err
	}
	zone := spec.TimeZone
	if zone == "" {
		zone = temporal.UTC
	}
	location, err := zone.Location()
	if err != nil {
		return err
	}
	d.location = location
	for i, column := range d.columns {
		if column.err != nil {
			return column.err
		}
		if column.assign == nil {
			return invalid("import column is not defined")
		}
		for _, previous := range d.columns[:i] {
			if strings.EqualFold(previous.heading, column.heading) {
				return fault.New(fault.Duplicate, "import column heading is repeated")
			}
		}
	}
	if len(spec.Rules) != 0 {
		d.rule, d.rules = validation.All(spec.Rules...), true
		if err := d.rule.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (d Definition[R]) Validate() error {
	if d.definition == nil {
		return invalid("import is not defined")
	}
	return d.definition.err
}

// Row is one valid parsed row and its 1-based row number: the worksheet row
// for XLSX, or the line on which the record starts for CSV.
type Row[R any] struct {
	Number int
	Value  R
}

// Failure reports the issues of one rejected row. Paths name the column
// heading (for parsing issues) or the row's validation field.
type Failure struct {
	Row    int              `json:"row"`
	Issues []contract.Issue `json:"issues"`
}

// Report summarizes an import. Rows counts non-blank data rows read; Imported
// counts rows accepted by the handler; Failed counts rejected rows, of which
// at most Limits.MaxFailures are retained in Failures.
type Report struct {
	Rows     int       `json:"rows"`
	Imported int       `json:"imported"`
	Failed   int       `json:"failed"`
	Failures []Failure `json:"failures,omitempty"`
}

// HeadingError reports a heading row that cannot be mapped: missing required
// headings or repeated declared headings. Nothing was passed to the handler.
type HeadingError struct{ issues []contract.Issue }

func (*HeadingError) Error() string              { return "import headings do not match the declaration" }
func (*HeadingError) Is(target error) bool       { return target == fault.Invalid }
func (e *HeadingError) Issues() []contract.Issue { return slices.Clone(e.issues) }

// Handler receives each chunk of valid rows in file order. The slice is owned
// by the handler. Returning an error stops the import.
type Handler[R any] func(context.Context, []Row[R]) error

// Run reads the source, maps and validates rows, and calls handle with chunks
// of valid rows. Invalid rows are reported and skipped; blank rows are
// ignored. Chunks already handled are not undone when a later chunk, a limit
// or the input fails: run the handler inside a caller-owned transaction and
// inspect Report.Failed to make an import all-or-nothing. Codec, rule and
// handler callbacks run synchronously in file order; panic/Goexit are faults.
func (d Definition[R]) Run(ctx context.Context, source Source, handle Handler[R]) (Report, error) {
	if err := d.Validate(); err != nil {
		return Report{}, err
	}
	if ctx == nil || handle == nil {
		return Report{}, invalid("import requires a context and a handler")
	}
	if err := source.validate(); err != nil {
		return Report{}, err
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	var report Report
	err := callback.Isolated("datatable import", func() error {
		var err error
		report, err = d.definition.run(ctx, source, handle)
		return err
	})
	return report, err
}

func (d *definition[R]) run(ctx context.Context, source Source, handle Handler[R]) (Report, error) {
	var report Report
	reader, err := source.open(ctx, d.limits)
	if err != nil {
		return report, err
	}
	defer reader.close()
	mapping, err := d.headings(reader)
	if err != nil {
		return report, err
	}
	chunk := make([]Row[R], 0, d.limits.ChunkSize)
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := handle(ctx, chunk); err != nil {
			return err
		}
		report.Imported += len(chunk)
		chunk = make([]Row[R], 0, d.limits.ChunkSize)
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		number, cells, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return report, err
		}
		if blank(cells) {
			continue
		}
		if report.Rows++; report.Rows > d.limits.MaxRows {
			return report, invalid("import exceeds its row limit")
		}
		row, issues, err := d.parse(ctx, mapping, cells)
		if err != nil {
			return report, err
		}
		if len(issues) != 0 {
			report.Failed++
			if len(report.Failures) < d.limits.MaxFailures {
				report.Failures = append(report.Failures, Failure{Row: number, Issues: issues})
			}
			continue
		}
		chunk = append(chunk, Row[R]{Number: number, Value: row})
		if len(chunk) == d.limits.ChunkSize {
			if err := flush(); err != nil {
				return report, err
			}
		}
	}
	return report, flush()
}

// headings reads up to the heading row and maps each declared column to its
// file position (-1 for an absent optional column).
func (d *definition[R]) headings(reader rowReader) ([]int, error) {
	for {
		number, cells, err := reader.next()
		if errors.Is(err, io.EOF) {
			return nil, invalid("import heading row is missing")
		}
		if err != nil {
			return nil, err
		}
		if number < d.heading {
			continue
		}
		if number > d.heading {
			return nil, invalid("import heading row is missing")
		}
		mapping := make([]int, len(d.columns))
		var issues []contract.Issue
		for i, column := range d.columns {
			mapping[i] = -1
			for position, cell := range cells {
				heading := strings.TrimSpace(strings.TrimPrefix(cell.text, "\uFEFF"))
				if !strings.EqualFold(heading, column.heading) {
					continue
				}
				if mapping[i] >= 0 {
					issues = append(issues, columnIssue(column, contract.KeyIssue))
					break
				}
				mapping[i] = position
			}
			if mapping[i] < 0 && column.required {
				issues = append(issues, columnIssue(column, contract.RequiredIssue))
			}
		}
		if len(issues) != 0 {
			return nil, &HeadingError{issues: issues}
		}
		return mapping, nil
	}
}

// parse converts mapped cells and then applies the row rules. Parsing issues
// skip rule checks, which would otherwise report misleading zero values.
func (d *definition[R]) parse(ctx context.Context, mapping []int, cells []rawCell) (R, []contract.Issue, error) {
	var row R
	var issues []contract.Issue
	for i, column := range d.columns {
		var text string
		if position := mapping[i]; position >= 0 && position < len(cells) {
			if len(cells[position].text) > d.limits.MaxCellBytes {
				issues = append(issues, columnIssue(column, contract.LengthIssue))
				continue
			}
			converted, ok := convert(cells[position], column.kind, d.location)
			if !ok {
				issues = append(issues, columnIssue(column, contract.TypeIssue))
				continue
			}
			text = converted
		}
		if text == "" && column.required {
			issues = append(issues, columnIssue(column, contract.RequiredIssue))
			continue
		}
		if err := column.assign(&row, text); err != nil {
			issues = append(issues, columnIssue(column, contract.TypeIssue))
		}
	}
	if len(issues) != 0 || !d.rules {
		return row, issues, nil
	}
	err := d.rule.Check(ctx, row, d.limits.Validation)
	var failed *validation.Errors
	if errors.As(err, &failed) {
		return row, failed.Issues(), nil
	}
	return row, nil, err
}

func columnIssue[R any](column Column[R], code contract.IssueCode) contract.Issue {
	return contract.Issue{Path: jsonpointer.Append("", column.heading), Code: code, Label: column.heading}
}

// blank reports a row without content. An error cell or an invalid shared
// string reference is content: its row is reported as a failure, not skipped.
func blank(cells []rawCell) bool {
	for _, cell := range cells {
		if cell.text != "" || cell.kind == rawInvalid {
			return false
		}
	}
	return true
}

func invalid(message string) error { return fault.New(fault.Invalid, message) }
