package docsource

import (
	"go/scanner"
	"go/token"
	"strings"
)

// lineComments records byte offsets, so marker-looking text in a literal or
// block comment cannot borrow the enclosing file's compiler proof.
func lineComments(data, language string) map[int]bool {
	comments := make(map[int]bool)
	if language == "go" {
		file := token.NewFileSet().AddFile("", -1, len(data))
		var scan scanner.Scanner
		scan.Init(file, []byte(data), func(token.Position, string) {}, scanner.ScanComments)
		for {
			pos, kind, literal := scan.Scan()
			if kind == token.EOF {
				return comments
			}
			if kind == token.COMMENT && strings.HasPrefix(literal, "//") {
				comments[file.Offset(pos)] = true
			}
		}
	}
	lexer := tsComments{data: data, comments: comments}
	lexer.code(0, 0)
	return comments
}

type tsComments struct {
	data     string
	comments map[int]bool
}

// code also scans template substitutions, returning after the matching brace.
func (s tsComments) code(i, braces int) int {
	expression := true
	control := false
	property := false
	var parentheses []bool
	for i < len(s.data) {
		c := s.data[i]
		switch {
		case strings.ContainsRune(" \t\r\n", rune(c)):
			i++
		case strings.HasPrefix(s.data[i:], "//"):
			s.comments[i] = true
			i = s.lineEnd(i)
		case strings.HasPrefix(s.data[i:], "/*"):
			end := strings.Index(s.data[i+2:], "*/")
			if end < 0 {
				return len(s.data)
			}
			i += end + 4
		case c == '\'' || c == '"' || c == '`':
			i = s.quoted(i)
			expression = false
		case c == '/' && expression:
			i = s.regexp(i)
			expression = false
		case strings.HasPrefix(s.data[i:], "++") || strings.HasPrefix(s.data[i:], "--"):
			// Postfix updates still end an operand; prefix updates still need one.
			i += 2
		case c == '(':
			parentheses = append(parentheses, control)
			control, expression = false, true
			i++
		case c == ')':
			expression = false
			if len(parentheses) > 0 {
				expression = parentheses[len(parentheses)-1]
				parentheses = parentheses[:len(parentheses)-1]
			}
			i++
		case c == '{':
			if braces > 0 {
				braces++
			}
			i++
			expression = true
		case c == '}':
			i++
			if braces > 0 {
				braces--
				if braces == 0 {
					return i
				}
			}
			expression = true
		case c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 128:
			start := i
			for i < len(s.data) && (s.data[i] == '_' || s.data[i] == '$' || s.data[i] >= 'a' && s.data[i] <= 'z' || s.data[i] >= 'A' && s.data[i] <= 'Z' || s.data[i] >= '0' && s.data[i] <= '9' || s.data[i] >= 128) {
				i++
			}
			if property {
				property, expression = false, false
				continue
			}
			switch s.data[start:i] {
			case "if", "while", "for", "with", "switch", "catch":
				control, expression = true, true
			case "return", "throw", "case", "delete", "void", "typeof", "yield", "await", "in", "of", "instanceof", "new", "else", "do":
				expression = true
			default:
				expression = false
			}
		default:
			i++
			property = c == '.'
			expression = c != ')' && c != ']' && c != '.'
		}
	}
	return i
}

func (s tsComments) quoted(i int) int {
	quote := s.data[i]
	for i++; i < len(s.data); {
		switch {
		case s.data[i] == '\\':
			i += 2
		case s.data[i] == quote:
			return i + 1
		case quote == '`' && strings.HasPrefix(s.data[i:], "${"):
			i = s.code(i+2, 1)
		default:
			i++
		}
	}
	return i
}

func (s tsComments) regexp(i int) int {
	start := i
	inClass := false
	for i++; i < len(s.data); i++ {
		switch s.data[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return i + 1
			}
		case '\n', '\r':
			return start + 1
		}
	}
	return start + 1
}

func (s tsComments) lineEnd(i int) int {
	if end := strings.IndexByte(s.data[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(s.data)
}
