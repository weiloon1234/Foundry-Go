package modelcheck

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// maxSQLBytes bounds one migration SQL string. A larger string is reported as
// unreadable instead of scanned.
const maxSQLBytes = 1 << 20

type sqlKind uint8

const (
	sqlWord   sqlKind = iota // bare identifier or keyword, lower-cased
	sqlIdent                 // "quoted identifier", content as written
	sqlString                // 'text' or E'text', decoded
	sqlNumber                // digits with an optional fraction or exponent
	sqlPunct                 // an operator or delimiter
	sqlDollar                // $tag$ body $tag$, body as written
)

// sqlToken is one lexical element of a migration statement. start and end are
// byte offsets in the scanned text; gap reports whitespace or a comment before it.
type sqlToken struct {
	kind       sqlKind
	text       string
	start, end int
	gap        bool
}

func (t sqlToken) is(kind sqlKind, text string) bool { return t.kind == kind && t.text == text }
func (t sqlToken) word(text string) bool             { return t.is(sqlWord, text) }
func (t sqlToken) punct(text string) bool            { return t.is(sqlPunct, text) }

// name reports a token that can name a table, column or constraint.
func (t sqlToken) name() bool { return t.kind == sqlWord || t.kind == sqlIdent }

var sqlOperators = []string{"->>", "#>>", "::", "<@", "@>", "<>", "!=", "<=", ">=", "||", "->", "#>", "?&", "?|", "&&"}

// scanSQL splits PostgreSQL text into tokens. It understands comments (block
// comments nest), quoted identifiers, standard and escape strings and
// dollar-quoted bodies, and fails on anything it cannot delimit.
func scanSQL(src string) ([]sqlToken, error) {
	if len(src) > maxSQLBytes {
		return nil, safeerror.New(fmt.Sprintf("SQL text is larger than %d bytes", maxSQLBytes))
	}
	var tokens []sqlToken
	gap := false
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
			gap = true
			continue
		case c == '-' && strings.HasPrefix(src[i:], "--"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				i = len(src)
			} else {
				i += end
			}
			gap = true
			continue
		case c == '/' && strings.HasPrefix(src[i:], "/*"):
			depth := 0
			j := i
			for j < len(src) {
				switch {
				case strings.HasPrefix(src[j:], "/*"):
					depth++
					j += 2
				case strings.HasPrefix(src[j:], "*/"):
					depth--
					j += 2
				default:
					j++
				}
				if depth == 0 {
					break
				}
			}
			if depth != 0 {
				return nil, safeerror.New(fmt.Sprintf("unterminated block comment at byte %d", i))
			}
			i = j
			gap = true
			continue
		}
		token := sqlToken{start: i, gap: gap}
		gap = false
		switch {
		case c == '\'':
			text, end, err := scanQuoted(src, i, '\'')
			if err != nil {
				return nil, err
			}
			token.kind, token.text, token.end = sqlString, text, end
		case (c == 'e' || c == 'E') && i+1 < len(src) && src[i+1] == '\'':
			text, end, err := scanEscapeString(src, i+1)
			if err != nil {
				return nil, err
			}
			token.kind, token.text, token.end = sqlString, text, end
		case (c == 'u' || c == 'U') && strings.HasPrefix(src[i+1:], "&'"):
			return nil, safeerror.New(fmt.Sprintf("unicode-escape string at byte %d is not supported", i))
		case c == '"':
			text, end, err := scanQuoted(src, i, '"')
			if err != nil {
				return nil, err
			}
			token.kind, token.text, token.end = sqlIdent, text, end
		case c == '$' && dollarTagEnd(src, i) > 0:
			open := dollarTagEnd(src, i)
			tag := src[i:open]
			close := strings.Index(src[open:], tag)
			if close < 0 {
				return nil, safeerror.New(fmt.Sprintf("unterminated dollar-quoted body at byte %d", i))
			}
			token.kind, token.text, token.end = sqlDollar, src[open:open+close], open+close+len(tag)
		case isSQLWordStart(c):
			j := i + 1
			for j < len(src) && isSQLWordPart(src[j]) {
				j++
			}
			token.kind, token.text, token.end = sqlWord, foldASCII(src[i:j]), j
		case c == '$' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i + 1
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			token.kind, token.text, token.end = sqlWord, src[i:j], j
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				k := j + 1
				if k < len(src) && (src[k] == '+' || src[k] == '-') {
					k++
				}
				if k < len(src) && src[k] >= '0' && src[k] <= '9' {
					for k < len(src) && src[k] >= '0' && src[k] <= '9' {
						k++
					}
					j = k
				}
			}
			token.kind, token.text, token.end = sqlNumber, src[i:j], j
		default:
			token.kind, token.end = sqlPunct, i+1
			for _, op := range sqlOperators {
				if strings.HasPrefix(src[i:], op) {
					token.end = i + len(op)
					break
				}
			}
			token.text = src[i:token.end]
		}
		tokens = append(tokens, token)
		i = token.end
	}
	return tokens, nil
}

// scanQuoted reads a quoted run whose opening quote is at src[open]. A doubled
// quote is one quote.
func scanQuoted(src string, open int, quote byte) (string, int, error) {
	var text strings.Builder
	for i := open + 1; i < len(src); i++ {
		switch {
		case src[i] == quote && i+1 < len(src) && src[i+1] == quote:
			text.WriteByte(quote)
			i++
		case src[i] == quote:
			return text.String(), i + 1, nil
		default:
			text.WriteByte(src[i])
		}
	}
	return "", 0, safeerror.New(fmt.Sprintf("unterminated quoted text at byte %d", open))
}

// scanEscapeString reads E'...' whose opening quote is at src[open] and decodes
// its backslash escapes as PostgreSQL does: \b \f \n \r \t, one to three octal
// digits, \x and one or two hex digits, \u and four or \U and eight hex digits
// (a \u surrogate pair is one character), and any other character after a
// backslash as itself. An escape that PostgreSQL rejects, a zero character and
// text that is not valid UTF-8 are errors, so a value is never mis-decoded.
func scanEscapeString(src string, open int) (string, int, error) {
	fail := func(why string, at int) (string, int, error) {
		return "", 0, safeerror.New(fmt.Sprintf("%s in the escape string at byte %d", why, at))
	}
	var text []byte
	for i := open + 1; i < len(src); {
		c := src[i]
		switch {
		case c == '\\':
			if i+1 >= len(src) {
				return fail("unterminated escape", open)
			}
			n := src[i+1]
			switch {
			case n == 'b' || n == 'f' || n == 'n' || n == 'r' || n == 't':
				text = append(text, map[byte]byte{'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}[n])
				i += 2
			case n >= '0' && n <= '7':
				value, j := 0, i+1
				for j < len(src) && j < i+4 && src[j] >= '0' && src[j] <= '7' {
					value = value*8 + int(src[j]-'0')
					j++
				}
				if value == 0 || value > 255 {
					return fail("an octal escape outside 1 to 255", i)
				}
				text = append(text, byte(value))
				i = j
			case n == 'x' && i+2 < len(src) && hexValue(src[i+2]) >= 0:
				value, j := 0, i+2
				for j < len(src) && j < i+4 && hexValue(src[j]) >= 0 {
					value = value*16 + hexValue(src[j])
					j++
				}
				if value == 0 {
					return fail("a zero character", i)
				}
				text = append(text, byte(value))
				i = j
			case n == 'u' || n == 'U':
				r, j, ok := readUnicodeEscape(src, i)
				if !ok {
					return fail("an invalid Unicode escape", i)
				}
				text = utf8.AppendRune(text, r)
				i = j
			default:
				text = append(text, n)
				i += 2
			}
		case c == '\'' && i+1 < len(src) && src[i+1] == '\'':
			text = append(text, '\'')
			i += 2
		case c == '\'':
			if !utf8.Valid(text) {
				return fail("text that is not valid UTF-8", open)
			}
			return string(text), i + 1, nil
		default:
			text = append(text, c)
			i++
		}
	}
	return fail("unterminated text", open)
}

// readUnicodeEscape reads \uXXXX, \UXXXXXXXX or a \u surrogate pair at src[i]
// and returns the character and the offset after the escape.
func readUnicodeEscape(src string, i int) (rune, int, bool) {
	digits := 4
	if src[i+1] == 'U' {
		digits = 8
	}
	read := func(at int) (rune, bool) {
		if at+2+digits > len(src) {
			return 0, false
		}
		var value rune
		for _, d := range []byte(src[at+2 : at+2+digits]) {
			h := hexValue(d)
			if h < 0 {
				return 0, false
			}
			value = value<<4 | rune(h)
		}
		return value, true
	}
	r, ok := read(i)
	if !ok || r == 0 {
		return 0, 0, false
	}
	end := i + 2 + digits
	if r >= 0xD800 && r <= 0xDBFF {
		if end+1 < len(src) && src[end] == '\\' && src[end+1] == 'u' && digits == 4 {
			if low, ok := read(end); ok && low >= 0xDC00 && low <= 0xDFFF {
				return 0x10000 + (r-0xD800)<<10 + (low - 0xDC00), end + 6, true
			}
		}
		return 0, 0, false
	}
	if r >= 0xDC00 && r <= 0xDFFF || r > utf8.MaxRune {
		return 0, 0, false
	}
	return r, end, true
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// foldASCII lower-cases ASCII letters only, as PostgreSQL folds an unquoted name.
func foldASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// dollarTagEnd returns the offset after the opening $tag$ at src[i], or 0 when
// the text does not open a dollar quote.
func dollarTagEnd(src string, i int) int {
	j := i + 1
	for j < len(src) && (isSQLWordPart(src[j]) && src[j] != '$') {
		j++
	}
	if j < len(src) && src[j] == '$' && (j == i+1 || !(src[i+1] >= '0' && src[i+1] <= '9')) {
		return j + 1
	}
	return 0
}

func isSQLWordStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isSQLWordPart(c byte) bool {
	return isSQLWordStart(c) || c >= '0' && c <= '9' || c == '$'
}

// splitStatements splits tokens at semicolons.
func splitStatements(tokens []sqlToken) [][]sqlToken {
	var result [][]sqlToken
	start := 0
	for i, token := range tokens {
		if token.punct(";") {
			if i > start {
				result = append(result, tokens[start:i])
			}
			start = i + 1
		}
	}
	if start < len(tokens) {
		result = append(result, tokens[start:])
	}
	return result
}

// maxNesting reports the deepest parenthesis, bracket and CASE nesting of the tokens.
func maxNesting(tokens []sqlToken) int {
	depth, deepest := 0, 0
	for _, t := range tokens {
		switch {
		case t.punct("(") || t.punct("[") || t.word("case"):
			depth++
			deepest = max(deepest, depth)
		case t.punct(")") || t.punct("]") || t.word("end"):
			depth--
		}
	}
	return deepest
}

// matching returns the index of the parenthesis or bracket that closes the one
// at tokens[open], or -1.
func matching(tokens []sqlToken, open int) int {
	depth := 0
	for i := open; i < len(tokens); i++ {
		if tokens[i].kind != sqlPunct {
			continue
		}
		switch tokens[i].text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevel splits tokens at commas outside any parenthesis or bracket.
func splitTopLevel(tokens []sqlToken) [][]sqlToken {
	var result [][]sqlToken
	depth, start := 0, 0
	for i, token := range tokens {
		if token.kind != sqlPunct {
			continue
		}
		switch token.text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		case ",":
			if depth == 0 {
				result = append(result, tokens[start:i])
				start = i + 1
			}
		}
	}
	return append(result, tokens[start:])
}
