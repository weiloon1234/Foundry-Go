package importer

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/datatable/internal/spreadsheet"
	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	// maxArchiveEntries bounds the ZIP central directory read before any
	// entry is opened; workbooks have a few dozen parts.
	maxArchiveEntries = 4096
	// maxMetadataBytes bounds each workbook/relationship part.
	maxMetadataBytes = 4 << 20
)

// xlsxReader streams one worksheet. Only the shared-string table is retained;
// every decompressed byte counts against one MaxXMLBytes budget.
type xlsxReader struct {
	ctx     context.Context
	files   map[string]*zip.File
	budget  *xmlBudget
	strings []string
	sheet   io.ReadCloser
	decoder *xml.Decoder
	limits  Limits
	row     int
}

func openXLSX(ctx context.Context, source Source, limits Limits) (reader *xlsxReader, err error) {
	if source.size > limits.MaxInputBytes {
		return nil, invalid("import input exceeds its byte limit")
	}
	if err := checkArchiveDirectory(source.at, source.size); err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(source.at, source.size)
	if err != nil {
		return nil, invalid("import XLSX is not a valid workbook")
	}
	r := &xlsxReader{ctx: ctx, files: make(map[string]*zip.File, len(archive.File)), budget: &xmlBudget{remaining: limits.MaxXMLBytes, maxRun: int64(limits.MaxCellBytes)*8 + 4096}, limits: limits}
	for _, file := range archive.File {
		if _, duplicate := r.files[file.Name]; duplicate {
			return nil, invalid("import XLSX repeats a part")
		}
		r.files[file.Name] = file
	}
	defer func() {
		if err != nil {
			r.close()
		}
	}()
	workbook, err := r.relationshipTarget("_rels/.rels", "", "/officeDocument")
	if err != nil {
		return nil, err
	}
	sheets, err := r.sheets(workbook)
	if err != nil {
		return nil, err
	}
	directory, base := path.Split(workbook)
	relationships, err := r.relationships(directory + "_rels/" + base + ".rels")
	if err != nil {
		return nil, err
	}
	var sheet string
	for _, candidate := range sheets {
		if source.sheet == "" || candidate.name == source.sheet {
			sheet = relationships.target(directory, candidate.id, "/worksheet")
			break
		}
	}
	if sheet == "" {
		return nil, invalid("import worksheet is missing")
	}
	if shared := relationships.firstOfType(directory, "/sharedStrings"); shared != "" {
		if r.strings, err = r.sharedStrings(shared); err != nil {
			return nil, err
		}
	}
	if r.sheet, err = r.open(sheet); err != nil {
		return nil, err
	}
	r.decoder = newDecoder(r.sheet)
	return r, nil
}

// checkArchiveDirectory reads the end-of-central-directory record before the
// ZIP reader allocates one entry per directory record.
func checkArchiveDirectory(at io.ReaderAt, size int64) error {
	const record, comment = 22, 65535
	window := min(size, record+comment)
	if window < record {
		return invalid("import XLSX is not a valid workbook")
	}
	tail := make([]byte, window)
	if _, err := at.ReadAt(tail, size-window); err != nil && !errors.Is(err, io.EOF) {
		return fault.Wrap(fault.Invalid, "import XLSX could not be read", err)
	}
	for i := len(tail) - record; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == 0x06054b50 {
			// Entries on this disk; 0xFFFF defers to ZIP64, which a bounded
			// workbook never needs.
			if entries := binary.LittleEndian.Uint16(tail[i+10:]); entries == 0xffff || entries > maxArchiveEntries {
				return invalid("import XLSX has too many parts")
			}
			return nil
		}
	}
	return invalid("import XLSX is not a valid workbook")
}

func (r *xlsxReader) close() {
	if r.sheet != nil {
		_ = r.sheet.Close()
		r.sheet = nil
	}
}

// open decompresses one part through the shared byte budget. The declared
// size is checked first, and actual bytes are counted as they are read.
func (r *xlsxReader) open(name string) (io.ReadCloser, error) {
	file := r.files[name]
	if file == nil {
		return nil, invalid("import XLSX part is missing")
	}
	if file.UncompressedSize64 > uint64(max(r.budget.remaining, 0)) {
		return nil, invalid("import XLSX exceeds its decompressed size limit")
	}
	body, err := file.Open()
	if err != nil {
		return nil, invalid("import XLSX part is invalid")
	}
	return &budgetReader{ctx: r.ctx, body: body, budget: r.budget}, nil
}

type xmlBudget struct {
	remaining, maxRun int64
}

// budgetReader counts decompressed bytes against the shared budget and bounds
// every XML token before encoding/xml buffers it. Its scanner follows the
// markup, so the run counter restarts only at a token boundary: a text node,
// a tag (whose quoted attribute values may contain '>'), a comment, a CDATA
// section or a processing instruction (which may contain '<' and '>') never
// grows beyond maxRun, whatever delimiters it contains. Document type
// declarations are rejected; OPC packages must not contain them.
type budgetReader struct {
	ctx    context.Context
	body   io.ReadCloser
	budget *xmlBudget
	scan   markupScanner
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.body.Read(p)
	if n < 0 || n > len(p) {
		return 0, fault.New(fault.Internal, "import reader returned an invalid count")
	}
	if b.budget.remaining -= int64(n); b.budget.remaining < 0 {
		return 0, invalid("import XLSX exceeds its decompressed size limit")
	}
	for _, c := range p[:n] {
		if failure := b.scan.step(c, b.budget.maxRun); failure != nil {
			return 0, failure
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, invalid("import XLSX part is invalid")
	}
	return n, err
}

type markupState uint8

const (
	scanText    markupState = iota // character data
	scanOpen                       // after '<'
	scanTag                        // element start or end tag
	scanQuote                      // quoted attribute value inside a tag
	scanBang                       // after "<!", classifying the declaration
	scanSection                    // comment, CDATA or processing instruction
)

// markupScanner is a minimal XML lexer: it classifies bytes only far enough to
// find token boundaries. encoding/xml still validates the document.
type markupScanner struct {
	state  markupState
	quote  byte
	bang   [7]byte
	banged int
	// A section ends at need or more closer bytes followed by '>': "-->",
	// "]]>" or "?>".
	closer     byte
	need, seen int
	run        int64
}

const (
	commentOpen = "--"
	cdataOpen   = "[CDATA["
)

// step consumes one byte. Delimiters that end a token restart the run; every
// other byte extends the current token and must stay within maxRun.
func (s *markupScanner) step(c byte, maxRun int64) error {
	boundary := false
	switch s.state {
	case scanText:
		if c == '<' {
			s.state, boundary = scanOpen, true
		}
	case scanOpen:
		switch c {
		case '!':
			s.state, s.banged = scanBang, 0
		case '?':
			s.section('?', 1)
		default:
			s.state = scanTag
			boundary = s.tag(c)
		}
	case scanTag:
		boundary = s.tag(c)
	case scanQuote:
		if c == s.quote {
			s.state = scanTag
		}
	case scanBang:
		s.bang[s.banged] = c
		s.banged++
		switch opened := string(s.bang[:s.banged]); {
		case opened == commentOpen:
			s.section('-', 2)
		case opened == cdataOpen:
			s.section(']', 2)
		case !strings.HasPrefix(commentOpen, opened) && !strings.HasPrefix(cdataOpen, opened):
			return invalid("import XLSX contains a document type declaration")
		}
	case scanSection:
		switch {
		case c == '>' && s.seen >= s.need:
			s.state, boundary = scanText, true
		case c == s.closer:
			s.seen++
		default:
			s.seen = 0
		}
	}
	if boundary {
		s.run = 0
	} else if s.run++; s.run > maxRun {
		return invalid("import XLSX contains an oversized value")
	}
	return nil
}

// tag consumes one byte of a start or end tag and reports whether it ended it.
func (s *markupScanner) tag(c byte) bool {
	switch c {
	case '"', '\'':
		s.state, s.quote = scanQuote, c
	case '>':
		s.state = scanText
		return true
	}
	return false
}
func (s *markupScanner) section(closer byte, need int) {
	s.state, s.closer, s.need, s.seen = scanSection, closer, need, 0
}
func (b *budgetReader) Close() error { return b.body.Close() }

func newDecoder(reader io.Reader) *xml.Decoder {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = true
	return decoder
}

// readMetadata decodes a small workbook part into target.
func (r *xlsxReader) readMetadata(name string, target any) error {
	file := r.files[name]
	if file == nil {
		return invalid("import XLSX part is missing")
	}
	if file.UncompressedSize64 > maxMetadataBytes {
		return invalid("import XLSX metadata is too large")
	}
	body, err := r.open(name)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := newDecoder(io.LimitReader(body, maxMetadataBytes)).Decode(target); err != nil {
		if errors.Is(err, fault.Invalid) {
			return err
		}
		return invalid("import XLSX metadata is malformed")
	}
	return nil
}

type relationships struct {
	Items []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
		Mode   string `xml:"TargetMode,attr"`
	} `xml:"Relationship"`
}

func (r *xlsxReader) relationships(name string) (relationships, error) {
	var result relationships
	return result, r.readMetadata(name, &result)
}
func (r *xlsxReader) relationshipTarget(name, directory, kind string) (string, error) {
	items, err := r.relationships(name)
	if err != nil {
		return "", err
	}
	if target := items.firstOfType(directory, kind); target != "" {
		return target, nil
	}
	return "", invalid("import XLSX workbook is missing")
}

// target resolves an internal relationship by ID and type suffix; the
// transitional and strict OOXML namespaces share the same suffixes.
func (items relationships) target(directory, id, kind string) string {
	for _, item := range items.Items {
		if item.ID == id && strings.HasSuffix(item.Type, kind) {
			return partName(directory, item.Target, item.Mode)
		}
	}
	return ""
}
func (items relationships) firstOfType(directory, kind string) string {
	for _, item := range items.Items {
		if strings.HasSuffix(item.Type, kind) {
			return partName(directory, item.Target, item.Mode)
		}
	}
	return ""
}

// partName resolves a relationship target inside the archive. External
// targets and paths escaping the package root are never opened.
func partName(directory, target, mode string) string {
	if mode != "" && mode != "Internal" || target == "" || strings.Contains(target, "://") {
		return ""
	}
	name := path.Clean(path.Join(directory, target))
	if strings.HasPrefix(target, "/") {
		name = path.Clean(strings.TrimPrefix(target, "/"))
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return ""
	}
	return name
}

type sheetEntry struct{ name, id string }

func (r *xlsxReader) sheets(workbook string) ([]sheetEntry, error) {
	var document struct {
		Sheets []struct {
			Name  string     `xml:"name,attr"`
			Attrs []xml.Attr `xml:",any,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := r.readMetadata(workbook, &document); err != nil {
		return nil, err
	}
	result := make([]sheetEntry, 0, len(document.Sheets))
	for _, sheet := range document.Sheets {
		entry := sheetEntry{name: sheet.Name}
		for _, attr := range sheet.Attrs {
			if attr.Name.Local == "id" && strings.HasSuffix(attr.Name.Space, "relationships") {
				entry.id = attr.Value
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

// sharedStrings loads the string table: plain and rich-text runs, without
// phonetic guides. Oversized entries keep one byte past the cell bound so the
// affected cell reports a length issue instead of failing the import.
func (r *xlsxReader) sharedStrings(name string) ([]string, error) {
	body, err := r.open(name)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	decoder := newDecoder(body)
	var result []string
	var total int64
	var text strings.Builder
	inItem, inText, phonetic := false, false, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, xmlError(err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem = true
				text.Reset()
			case "rPh":
				phonetic++
			case "t":
				inText = inItem && phonetic == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				inItem = false
				if len(result) >= r.limits.MaxSharedStrings {
					return nil, invalid("import XLSX has too many shared strings")
				}
				if total += int64(text.Len()); total > r.limits.MaxSharedStringBytes {
					return nil, invalid("import XLSX shared strings exceed their byte limit")
				}
				result = append(result, spreadsheet.DecodeText(text.String()))
			case "rPh":
				phonetic--
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				appendBounded(&text, t, r.limits.MaxCellBytes)
			}
		}
	}
}

// appendBounded keeps at most maximum+1 bytes: enough to detect an oversized
// cell without retaining it.
func appendBounded(builder *strings.Builder, data []byte, maximum int) {
	if room := maximum + 1 - builder.Len(); room > 0 {
		builder.Write(data[:min(len(data), room)])
	}
}

func xmlError(err error) error {
	if errors.Is(err, fault.Invalid) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return invalid("import XLSX worksheet is malformed")
}

// next returns the next <row> of the worksheet. Missing rows are simply
// absent; missing cells between present ones are empty.
func (r *xlsxReader) next() (int, []rawCell, error) {
	for {
		token, err := r.decoder.Token()
		if errors.Is(err, io.EOF) {
			return 0, nil, io.EOF
		}
		if err != nil {
			return 0, nil, xmlError(err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "row" {
			return r.readRow(start)
		}
	}
}

func (r *xlsxReader) readRow(start xml.StartElement) (int, []rawCell, error) {
	number := r.row + 1
	if reference := attribute(start, "r"); reference != "" {
		parsed, err := strconv.Atoi(reference)
		if err != nil || parsed <= r.row || parsed > spreadsheet.MaxRows {
			return 0, nil, invalid("import XLSX row order is invalid")
		}
		number = parsed
	}
	r.row = number
	var cells []rawCell
	for {
		token, err := r.decoder.Token()
		if err != nil {
			return 0, nil, xmlError(unexpectedEOF(err))
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local != "c" {
				if err := r.decoder.Skip(); err != nil {
					return 0, nil, xmlError(unexpectedEOF(err))
				}
				continue
			}
			column := len(cells) + 1
			if reference := attribute(t, "r"); reference != "" {
				parsed, row, ok := spreadsheet.ParseReference(reference)
				if !ok || row != number || parsed < column {
					return 0, nil, invalid("import XLSX cell order is invalid")
				}
				column = parsed
			}
			if column > r.limits.MaxColumns {
				return 0, nil, invalid("import row exceeds its column limit")
			}
			cell, err := r.readCell(t)
			if err != nil {
				return 0, nil, err
			}
			for len(cells) < column-1 {
				cells = append(cells, rawCell{})
			}
			cells = append(cells, cell)
		case xml.EndElement:
			return number, cells, nil
		}
	}
}

// readCell reads one <c> element: the stored <v> value and any inline string.
func (r *xlsxReader) readCell(start xml.StartElement) (rawCell, error) {
	kind := attribute(start, "t")
	var stored, inline strings.Builder
	depth, inValue, inText, phonetic := 0, false, false, 0
	for {
		token, err := r.decoder.Token()
		if err != nil {
			return rawCell{}, xmlError(unexpectedEOF(err))
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "v":
				inValue = depth == 1
			case "rPh":
				phonetic++
			case "t":
				inText = phonetic == 0
			}
		case xml.EndElement:
			if depth == 0 {
				return r.cellValue(kind, stored.String(), inline.String()), nil
			}
			depth--
			switch t.Name.Local {
			case "v":
				inValue = false
			case "rPh":
				phonetic--
			case "t":
				inText = false
			}
		case xml.CharData:
			if inValue {
				appendBounded(&stored, t, r.limits.MaxCellBytes)
			} else if inText {
				appendBounded(&inline, t, r.limits.MaxCellBytes)
			}
		}
	}
}

func (r *xlsxReader) cellValue(kind, stored, inline string) rawCell {
	switch kind {
	case "s":
		index, err := strconv.Atoi(stored)
		if err != nil || index < 0 || index >= len(r.strings) {
			return rawCell{kind: rawInvalid}
		}
		return rawCell{text: r.strings[index]}
	case "inlineStr":
		return rawCell{text: spreadsheet.DecodeText(inline)}
	case "str", "d":
		return rawCell{text: spreadsheet.DecodeText(stored)}
	case "b":
		return rawCell{text: stored, kind: rawBoolean}
	case "e":
		return rawCell{kind: rawInvalid}
	case "", "n":
		return rawCell{text: stored, kind: rawNumber}
	}
	return rawCell{kind: rawInvalid}
}

func attribute(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == name && attr.Name.Space == "" {
			return attr.Value
		}
	}
	return ""
}
func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return invalid("import XLSX worksheet is truncated")
	}
	return err
}
