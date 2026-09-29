// Package spreadsheet owns the SpreadsheetML (XLSX) conventions shared by
// datatable exports and imports: cell references, OOXML text escapes and
// serial dates. It performs no I/O.
package spreadsheet

import (
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
)

const (
	// MaxRows and MaxColumns are the worksheet dimensions of the format.
	MaxRows    = 1_048_576
	MaxColumns = 16_384
	// MaxCellUTF16Units is the text length limit of one cell.
	MaxCellUTF16Units = 32_767
	// ExactDigits is the precision spreadsheets store and display exactly.
	ExactDigits = 15
)

// Kind classifies a scalar wire type for spreadsheet storage. Text is always
// safe; typed kinds become numbers, booleans or dates when exactly representable.
type Kind uint8

const (
	Text Kind = iota
	Integer
	Number
	Decimal
	Boolean
	Date
	DateTime
	LocalDateTime
)

// KindOf derives the storage kind of one scalar contract type. Enumerations
// and every other string format remain text.
func KindOf(typ contract.Type) Kind {
	switch {
	case len(typ.Cases) != 0:
		return Text
	case typ.Kind == contract.IntegerKind:
		return Integer
	case typ.Kind == contract.NumberKind:
		return Number
	case typ.Kind == contract.BooleanKind:
		return Boolean
	case typ.Kind != contract.StringKind:
		return Text
	}
	switch typ.Format {
	case contract.DecimalFormat:
		return Decimal
	case contract.DateFormat:
		return Date
	case contract.DateTimeFormat:
		return DateTime
	case contract.LocalDateTimeFormat:
		return LocalDateTime
	}
	return Text
}

// LocalDateTimeLayout is the wall-clock text layout of LocalDateTime values.
const LocalDateTimeLayout = "2006-01-02T15:04:05.999999999"

// AppendColumn appends the column letters of 1-based index n (1 is A).
func AppendColumn(dst []byte, n int) []byte {
	var buffer [4]byte
	i := len(buffer)
	for n > 0 {
		n--
		i--
		buffer[i] = byte('A' + n%26)
		n /= 26
	}
	return append(dst, buffer[i:]...)
}

// ParseReference returns the 1-based column and row of a reference such as
// "B3". ok is false for malformed references or references outside a sheet.
func ParseReference(reference string) (column, row int, ok bool) {
	i := 0
	for i < len(reference) && reference[i] >= 'A' && reference[i] <= 'Z' {
		column = column*26 + int(reference[i]-'A'+1)
		if i++; i > 3 || column > MaxColumns {
			return 0, 0, false
		}
	}
	if i == 0 || i == len(reference) || reference[i] == '0' {
		return 0, 0, false
	}
	for ; i < len(reference); i++ {
		c := reference[i]
		if c < '0' || c > '9' {
			return 0, 0, false
		}
		if row = row*10 + int(c-'0'); row > MaxRows {
			return 0, 0, false
		}
	}
	return column, row, true
}

// AppendText XML-escapes cell text and applies OOXML's _xHHHH_ escape to
// characters XML cannot carry (C0 controls other than tab/newline and
// U+FFFE/U+FFFF) and to carriage returns, which XML parsers would normalize.
// Every literal "_x" prefix is protected, including incomplete sequences:
// expanding a later character inserts an underscore and could otherwise finish
// a new escape, for example "_x0000\r" becoming "_x0000_x000D_".
func AppendText(dst []byte, text string) []byte {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == '_' && i+1 < len(text) && text[i+1] == 'x':
			dst = append(dst, "_x005F_"...)
		case r == '<':
			dst = append(dst, "&lt;"...)
		case r == '>':
			dst = append(dst, "&gt;"...)
		case r == '&':
			dst = append(dst, "&amp;"...)
		case r < 0x20 && r != '\t' && r != '\n' || r == 0xfffe || r == 0xffff:
			dst = appendEscape(dst, uint16(r))
		case r == utf8.RuneError && size == 1:
			dst = utf8.AppendRune(dst, utf8.RuneError)
		default:
			dst = append(dst, text[i:i+size]...)
		}
		i += size
	}
	return dst
}
func appendEscape(dst []byte, v uint16) []byte {
	const digits = "0123456789ABCDEF"
	return append(dst, '_', 'x', digits[v>>12], digits[v>>8&0xf], digits[v>>4&0xf], digits[v&0xf], '_')
}

// DecodeText reverses the single-pass OOXML _xHHHH_ escape of stored text.
// Escaped underscores do not recursively decode a later literal sequence.
func DecodeText(text string) string {
	if !containsEscape(text) {
		return text
	}
	result := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if v, ok := escapeAt(text, i); ok {
			result = utf8.AppendRune(result, rune(v))
			i += 7
			continue
		}
		result = append(result, text[i])
		i++
	}
	return string(result)
}
func containsEscape(text string) bool {
	for i := 0; i+7 <= len(text); i++ {
		if _, ok := escapeAt(text, i); ok {
			return true
		}
	}
	return false
}
func escapeAt(text string, i int) (uint64, bool) {
	if i+7 > len(text) || text[i] != '_' || text[i+1] != 'x' || text[i+6] != '_' {
		return 0, false
	}
	v, err := strconv.ParseUint(text[i+2:i+6], 16, 16)
	return v, err == nil
}

// Serial dates count days from 1899-12-30. The 1900 leap-year quirk makes this
// epoch correct from 1900-03-01, the first supported day.
var (
	epoch    = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	firstDay = time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)
	endDay   = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Serial returns the serial number of a wall-clock time (its location is
// ignored). ok is false outside 1900-03-01 through 9999-12-31.
func Serial(wall time.Time) (string, bool) {
	wall = time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), time.UTC)
	if wall.Before(firstDay) || !wall.Before(endDay) {
		return "", false
	}
	elapsed := wall.Sub(epoch)
	days := int64(elapsed / (24 * time.Hour))
	remainder := elapsed % (24 * time.Hour)
	if remainder == 0 {
		return strconv.FormatInt(days, 10), true
	}
	return strconv.FormatFloat(float64(days)+remainder.Seconds()/86400, 'f', -1, 64), true
}

// Time converts a serial number to UTC wall-clock time, rounded to the
// millisecond resolution spreadsheets store. ok is false outside the range
// Serial produces.
func Time(serial float64) (time.Time, bool) {
	if math.IsNaN(serial) || math.IsInf(serial, 0) {
		return time.Time{}, false
	}
	days := math.Floor(serial)
	milliseconds := math.Round((serial - days) * 86_400_000)
	wall := epoch.AddDate(0, 0, int(days)).Add(time.Duration(milliseconds) * time.Millisecond)
	if days < 61 || wall.Before(firstDay) || !wall.Before(endDay) {
		return time.Time{}, false
	}
	return wall, true
}

// DisplayNumber returns the text of a stored double that a spreadsheet shows
// without rounding: at most ExactDigits significant digits, written without an
// exponent. ok is false when rounding to ExactDigits would change the stored
// value, for example a 16-digit identifier or 0.30000000000000004, so an
// importer reports it instead of silently using a different number.
func DisplayNumber(text string) (string, bool) {
	f, ok := storedNumber(text)
	if !ok {
		return "", false
	}
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(f, 'g', ExactDigits, 64), 64)
	if err != nil || rounded != f {
		return "", false
	}
	return plainNumber(f), true
}

// ExactNumber returns the shortest exponent-free text that parses back to the
// stored double, for floating-point columns that receive the value itself.
func ExactNumber(text string) (string, bool) {
	f, ok := storedNumber(text)
	if !ok {
		return "", false
	}
	return plainNumber(f), true
}

func storedNumber(text string) (float64, bool) {
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
func plainNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
