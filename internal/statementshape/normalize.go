// Package statementshape normalizes bounded untrusted SQL for keyed grouping only.
// Its output must never be displayed, logged, or treated as trusted SQL.
package statementshape

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 16 * 1024

func Normalize(sql string) (string, bool) {
	if len(sql) > MaxBytes || !utf8.ValidString(sql) {
		return "", false
	}
	tokens, ok := shapeTokens(sql)
	if !ok {
		return "", false
	}
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		out = append(out, token)
		if token != "in" && token != "any" && token != "values" {
			continue
		}
		if i+1 >= len(tokens) || tokens[i+1] != "(" {
			continue
		}
		end := shapeGroupEnd(tokens, i+1)
		if end < 0 {
			return "", false
		}
		shapes, ok := literalList(tokens[i+2:end], token != "values", token == "any")
		if !ok {
			continue
		}
		row := "( " + strings.Join(shapes, " , ") + " )"
		out = append(out, row)
		i = end
		if token == "values" {
			rows := []string{row}
			for i+2 < len(tokens) && tokens[i+1] == "," && tokens[i+2] == "(" {
				end = shapeGroupEnd(tokens, i+2)
				if end < 0 {
					return "", false
				}
				next, valid := literalList(tokens[i+3:end], false, false)
				if !valid {
					break
				}
				row = "( " + strings.Join(next, " , ") + " )"
				if !containsShape(rows, row) {
					rows = append(rows, row)
				}
				i = end
			}
			out[len(out)-1] = strings.Join(rows, " , ")
		}
	}
	result := strings.Join(out, " ")
	if len(result) > MaxBytes {
		return "", false
	}
	return result, true
}

func containsShape(shapes []string, s string) bool {
	for _, x := range shapes {
		if x == s {
			return true
		}
	}
	return false
}
func shapeGroupEnd(tokens []string, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i] {
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

// literalList preserves ordered positions for VALUES and deduplicates signatures
// only for IN/ANY. Neither identifiers nor arbitrary function calls are literals.
func literalList(tokens []string, unique, unwrapArray bool) ([]string, bool) {
	var shapes []string
	start, depth := 0, 0
	for i := 0; i <= len(tokens); i++ {
		if i == len(tokens) || tokens[i] == "," && depth == 0 {
			shape, ok := literalShape(tokens[start:i], unwrapArray)
			if !ok {
				return nil, false
			}
			if !unique || !containsShape(shapes, shape) {
				shapes = append(shapes, shape)
			}
			start = i + 1
		} else {
			switch tokens[i] {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
		}
	}
	return shapes, len(shapes) > 0
}
func literalShape(tokens []string, unwrapArray bool) (string, bool) {
	if len(tokens) == 0 {
		return "", false
	}
	i, shape := 0, "?"
	if tokens[i] == "+" || tokens[i] == "-" {
		i++
		if i == len(tokens) {
			return "", false
		}
	}
	switch tokens[i] {
	case "?":
		i++
	case "(":
		end := shapeGroupEnd(tokens, i)
		if end < 0 {
			return "", false
		}
		inner, ok := literalShape(tokens[i+1:end], unwrapArray)
		if !ok {
			return "", false
		}
		shape, i = inner, end+1
	case "array":
		if i+1 >= len(tokens) || tokens[i+1] != "[" {
			return "", false
		}
		end := shapeGroupEnd(tokens, i+1)
		if end < 0 {
			return "", false
		}
		inner, ok := literalList(tokens[i+2:end], true, false)
		if !ok {
			return "", false
		}
		shape, i = "array [ "+strings.Join(inner, " , ")+" ]", end+1
		// Only ANY treats an array operand as a list of candidate values. VALUES
		// and IN retain the container even when every element has one signature.
		if unwrapArray {
			shape = strings.Join(inner, " , ")
		}

	case "cast":
		if i+1 >= len(tokens) || tokens[i+1] != "(" {
			return "", false
		}
		end := shapeGroupEnd(tokens, i+1)
		if end < 0 {
			return "", false
		}
		depth, as := 0, -1
		for j := i + 2; j < end; j++ {
			switch tokens[j] {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			case "as":
				if depth == 0 {
					as = j
				}
			}
		}
		if as < 0 {
			return "", false
		}
		inner, ok := literalShape(tokens[i+2:as], unwrapArray)
		if !ok {
			return "", false
		}
		typ, ok := shapeCastType(tokens[as+1 : end])
		if !ok {
			return "", false
		}
		shape, i = inner+" :: "+typ, end+1
	default:
		return "", false
	}
	for i < len(tokens) {
		if tokens[i] != "::" {
			return "", false
		}
		start := i + 1
		i = start
		depth := 0
		for i < len(tokens) {
			if tokens[i] == "::" && depth == 0 {
				break
			}
			switch tokens[i] {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
			i++
		}
		typ, ok := shapeCastType(tokens[start:i])
		if !ok {
			return "", false
		}
		shape += " :: " + typ
	}
	return shape, true
}
func shapeCastType(tokens []string) (string, bool) {
	if len(tokens) == 0 || !shapeIdentifier(tokens[0]) {
		return "", false
	}
	for _, t := range tokens {
		if shapeIdentifier(t) {
			continue
		}
		switch t {
		case ".", "[", "]", "(", ")", "?", ",":
		default:
			return "", false
		}
	}
	return strings.Join(tokens, " "), true
}
func shapeIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r == '"' || r == '_' || unicode.IsLetter(r)
}
func shapeTokens(sql string) ([]string, bool) {
	var tokens []string
	stack := make([]byte, 0, 8)
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' {
			i++
			continue
		}
		if strings.HasPrefix(sql[i:], "--") {
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
			continue
		}
		if strings.HasPrefix(sql[i:], "/*") {
			i += 2
			depth := 1
			for i < len(sql) && depth > 0 {
				if strings.HasPrefix(sql[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(sql[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, false
			}
			continue
		}
		if i+2 < len(sql) && (c == 'u' || c == 'U') && sql[i+1] == '&' && sql[i+2] == '"' {
			return nil, false
		}
		// Prefixes E/B/X/N and U& introduce literal strings, never identifiers.
		prefix := 0
		if i+1 < len(sql) && strings.ContainsRune("eEbBxXnN", rune(c)) && sql[i+1] == '\'' {
			prefix = 1
		}
		if i+2 < len(sql) && (c == 'u' || c == 'U') && sql[i+1] == '&' && sql[i+2] == '\'' {
			prefix = 2
		}
		if c == '\'' || prefix > 0 {
			escaped := prefix == 1 && (c == 'e' || c == 'E')
			i += prefix + 1
			closed := false
			for i < len(sql) {
				if sql[i] == '\\' {
					// Without an explicit E prefix, server string settings can
					// change where this literal ends. Never guess in diagnostics.
					if !escaped {
						return nil, false
					}
					i += 2
					continue
				}
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
			tokens = append(tokens, "?")
			continue
		}
		if c == '"' {
			start := i
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '"' {
					i++
					if i < len(sql) && sql[i] == '"' {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
			tokens = append(tokens, sql[start:i])
			continue
		}
		if c == '$' {
			end := i + 1
			for end < len(sql) && (sql[end] == '_' || sql[end] >= 'a' && sql[end] <= 'z' || sql[end] >= 'A' && sql[end] <= 'Z' || end > i+1 && sql[end] >= '0' && sql[end] <= '9') {
				end++
			}
			if end < len(sql) && sql[end] == '$' {
				delimiter := sql[i : end+1]
				start := end + 1
				close := strings.Index(sql[start:], delimiter)
				if close < 0 {
					return nil, false
				}
				i = start + close + len(delimiter)
				tokens = append(tokens, "?")
				continue
			}
			end = i + 1
			for end < len(sql) && sql[end] >= '0' && sql[end] <= '9' {
				end++
			}
			if end == i+1 {
				return nil, false
			}
			i = end
			tokens = append(tokens, "?")
			continue
		}
		if (c == '+' || c == '-') && i+1 < len(sql) && (sql[i+1] >= '0' && sql[i+1] <= '9' || sql[i+1] == '.') {
			unary := len(tokens) == 0
			if len(tokens) > 0 {
				switch tokens[len(tokens)-1] {
				case "(", "[", ",", "=", ">", "<", "+", "-", "*", "/", "select", "then", "else":
					unary = true
				}
			}
			if unary {
				i++
				continue
			}
		}
		if c >= '0' && c <= '9' || c == '.' && i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9' {
			i++
			for i < len(sql) {
				x := sql[i]
				if x >= '0' && x <= '9' || x == '.' || x == '_' || x >= 'a' && x <= 'f' || x >= 'A' && x <= 'F' || x == 'x' || x == 'X' || x == 'o' || x == 'O' {
					i++
					continue
				}
				if (x == '+' || x == '-') && (sql[i-1] == 'e' || sql[i-1] == 'E') {
					i++
					continue
				}
				break
			}
			tokens = append(tokens, "?")
			continue
		}
		r, size := utf8.DecodeRuneInString(sql[i:])
		if r == '_' || unicode.IsLetter(r) {
			start := i
			i += size
			for i < len(sql) {
				r, size = utf8.DecodeRuneInString(sql[i:])
				if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				i += size
			}
			word := strings.ToLower(sql[start:i])
			if word == "null" || word == "true" || word == "false" {
				word = "?"
			}
			tokens = append(tokens, word)
			continue
		}
		if strings.HasPrefix(sql[i:], "::") {
			tokens = append(tokens, "::")
			i += 2
			continue
		}
		if c == '(' || c == '[' {
			stack = append(stack, c)
			if len(stack) > 64 {
				return nil, false
			}
		}
		if c == ')' || c == ']' {
			if len(stack) == 0 || c == ')' && stack[len(stack)-1] != '(' || c == ']' && stack[len(stack)-1] != '[' {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		}
		if !strings.ContainsRune("()[],.;:?+-*/<>=~!@#%^&|", rune(c)) {
			return nil, false
		}
		tokens = append(tokens, string(c))
		i++
	}
	return tokens, len(stack) == 0 && len(tokens) > 0
}
