package querycore

import (
	"database/sql/driver"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// RawFragment owns lexed SQL and normalized arguments. Private state prevents
// bypassing construction validation or mutating a fragment through an AST copy.
type RawFragment struct{ state *rawFragmentState }

type rawFragmentState struct {
	parts     []string
	arguments []driver.Value
	err       error
}

func (RawFragment) ValueNode()      {}
func (RawFragment) ExpressionNode() {}

func NewRawFragment(sql string, arguments []any) RawFragment {
	parts, err := rawParts(sql)
	r := RawFragment{state: &rawFragmentState{parts: parts, err: err}}
	if err != nil {
		return r
	}
	if len(arguments) > MaxParameters || len(parts) != len(arguments)+1 {
		r.state.err = rawInvalid("placeholder count does not match arguments")
		return r
	}
	r.state.arguments = make([]driver.Value, len(arguments))
	for i, arg := range arguments {
		v, err := driver.DefaultParameterConverter.ConvertValue(arg)
		if err != nil || !driver.IsValue(v) {
			r.state.err = rawInvalid("unsupported argument")
			return r
		}
		if b, ok := v.([]byte); ok {
			v = slices.Clone(b)
		}
		r.state.arguments[i] = v
	}
	return r
}
func rawInvalid(message string) error { return fault.New(fault.Invalid, "raw fragment: "+message) }
func (r RawFragment) Validate() error {
	if r.state == nil {
		return rawInvalid("uninitialized fragment")
	}
	if r.state.err != nil {
		return r.state.err
	}
	if len(r.state.parts) == 0 {
		return rawInvalid("uninitialized fragment")
	}
	return nil
}
func (c *Compiler) RawSQL(r RawFragment) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	if c.IndexTarget {
		return "", rawInvalid("not allowed in schema expressions")
	}
	for _, part := range r.state.parts {
		c.ScalarSQLBytes += len(part)
	}
	if c.ScalarSQLBytes > MaxScalarSQLBytes {
		return "", rawInvalid("SQL exceeds its resource bound")
	}
	c.opaque = true
	var out strings.Builder
	out.WriteByte('(')
	for i, part := range r.state.parts {
		if i > 0 {
			p, err := c.Parameter(r.state.arguments[i-1])
			if err != nil {
				return "", err
			}
			out.WriteString(p)
		}
		out.WriteString(part)
	}
	// A final line comment must not consume the enclosing query's syntax.
	out.WriteString("\n)")
	return out.String(), nil
}

// rawParts recognizes PostgreSQL lexical regions, not SQL grammar. Values are
// never interpolated. Question marks inside lexical regions remain untouched.
func rawParts(sql string) ([]string, error) {
	if len(sql) == 0 || len(sql) > MaxScalarSQLBytes || strings.IndexByte(sql, 0) >= 0 || !utf8.ValidString(sql) {
		return nil, rawInvalid("invalid SQL text or size")
	}
	var part strings.Builder
	var parts []string
	depth := 0
	meaningful := false
	for i := 0; i < len(sql); {
		start := i
		switch ch := sql[i]; {
		case ch == '\'' || ch == '"':
			escape := ch == '\'' && i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i == 1 || !rawIdentifier(sql[i-2]))
			i++
			closed := false
			for i < len(sql) {
				if ch == '\'' && !escape && sql[i] == '\\' {
					return nil, rawInvalid("backslash in standard string literal is ambiguous under standard_conforming_strings; use E'...' or a parameter")
				}
				if escape && sql[i] == '\\' {
					i += 2
					continue
				}
				if sql[i] == ch {
					i++
					if i < len(sql) && sql[i] == ch {
						i++
						continue
					}
					if ch == '\'' {
						if next := rawStringContinuation(sql, i); next != i {
							i = next
							continue
						}
					}
					closed = true
					break
				}
				i++
			}
			if !closed || i > len(sql) {
				return nil, rawInvalid("unterminated quoted region")
			}
			meaningful = true
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i += 2
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			n := 1
			for i < len(sql) && n > 0 {
				if i+1 < len(sql) && sql[i:i+2] == "/*" {
					n++
					if n > MaxExpressionDepth {
						return nil, rawInvalid("comment nesting exceeds its bound")
					}
					i += 2
				} else if i+1 < len(sql) && sql[i:i+2] == "*/" {
					n--
					i += 2
				} else {
					i++
				}
			}
			if n != 0 {
				return nil, rawInvalid("unterminated comment")
			}
		case ch == '$':
			j := i + 1
			if j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				return nil, rawInvalid("numbered placeholders are not allowed")
			}
			for j < len(sql) && rawTag(sql[j]) {
				j++
			}
			if j >= len(sql) || sql[j] != '$' {
				return nil, rawInvalid("malformed dollar quote or placeholder")
			}
			tag := sql[i : j+1]
			end := strings.Index(sql[j+1:], tag)
			if end < 0 {
				return nil, rawInvalid("unterminated dollar quote")
			}
			i = j + 1 + end + len(tag)
			meaningful = true
		case ch == '?':
			meaningful = true
			i++
			if i < len(sql) && sql[i] == '?' {
				part.WriteByte('?')
				i++
				continue
			}
			if i < len(sql) && rawIdentifier(sql[i]) {
				return nil, rawInvalid("malformed question-mark placeholder")
			}
			parts = append(parts, part.String())
			part.Reset()
			if len(parts) > MaxParameters {
				return nil, rawInvalid("too many placeholders")
			}
			continue
		case ch == ';':
			return nil, rawInvalid("statement separators are not allowed")
		case ch == ':' && i+1 < len(sql) && sql[i+1] == ':':
			i += 2
			meaningful = true
		case (ch == ':' || ch == '@') && i+1 < len(sql) && rawIdentifier(sql[i+1]):
			return nil, rawInvalid("named placeholders are not allowed")
		case ch == '(':
			depth++
			if depth > MaxExpressionDepth {
				return nil, rawInvalid("parentheses exceed their bound")
			}
			i++
			meaningful = true
		case ch == ')':
			depth--
			if depth < 0 {
				return nil, rawInvalid("unbalanced parentheses")
			}
			i++
			meaningful = true
		case rawTagStart(ch):
			i++
			for i < len(sql) && rawIdentifier(sql[i]) {
				i++
			}
			meaningful = true
		default:
			i++
			if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
				meaningful = true
			}
		}
		part.WriteString(sql[start:i])
	}
	if depth != 0 || !meaningful {
		return nil, rawInvalid("empty fragment or unbalanced parentheses")
	}
	return append(parts, part.String()), nil
}
func rawTagStart(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 128
}
func rawTag(b byte) bool        { return rawTagStart(b) || b >= '0' && b <= '9' }
func rawIdentifier(b byte) bool { return rawTag(b) || b == '$' }

// PostgreSQL continues a string across newline-containing whitespace (including
// line comments), retaining the first segment's escape mode. Block comments and
// an E prefix start separate tokens rather than continuing the current string.
func rawStringContinuation(sql string, end int) int {
	newline := false
	for i := end; i < len(sql); {
		switch sql[i] {
		case ' ', '\t', '\f', '\v':
			i++
		case '\n', '\r':
			newline = true
			i++
		case '-':
			if i+1 >= len(sql) || sql[i+1] != '-' {
				return end
			}
			i += 2
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case '\'':
			if newline {
				return i + 1
			}
			return end
		default:
			return end
		}
	}
	return end
}
