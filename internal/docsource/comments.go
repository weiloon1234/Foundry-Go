package docsource

import (
	"go/scanner"
	"go/token"
	"strings"
)

// span is a half-open byte range of a source file.
type span struct{ start, end int }

// lineComments records byte offsets, so marker-looking text in a literal or
// block comment cannot borrow the enclosing file's compiler proof. For tsx it
// also returns the byte ranges of JSX elements: a line comment cannot start a
// region there, because inside JSX text a double slash is text.
func lineComments(data, language string) (map[int]bool, []span) {
	comments := make(map[int]bool)
	if language == "go" {
		file := token.NewFileSet().AddFile("", -1, len(data))
		var scan scanner.Scanner
		scan.Init(file, []byte(data), func(token.Position, string) {}, scanner.ScanComments)
		for {
			pos, kind, literal := scan.Scan()
			if kind == token.EOF {
				return comments, nil
			}
			if kind == token.COMMENT && strings.HasPrefix(literal, "//") {
				comments[file.Offset(pos)] = true
			}
		}
	}
	var elements []span
	lexer := tsComments{data: data, comments: comments, tsx: language == "tsx", elements: &elements}
	lexer.code(0, 0)
	return comments, elements
}

type tsComments struct {
	data     string
	comments map[int]bool
	// tsx enables JSX elements; elements collects their ranges, and trail
	// lists the comments found so a failed JSX attempt can be undone.
	tsx      bool
	elements *[]span
	trail    []int
}

func (s *tsComments) comment(i int) {
	s.comments[i] = true
	s.trail = append(s.trail, i)
}

// code also scans template substitutions, returning after the matching brace.
func (s *tsComments) code(i, braces int) int {
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
			s.comment(i)
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
		case c == '<' && expression && s.tsx && i+1 < len(s.data) && (s.data[i+1] == '>' || identifierStart(s.data[i+1])):
			if end := s.element(i); end > i {
				i = end
				expression = false
			} else {
				i++
				expression = true
			}
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

func (s *tsComments) quoted(i int) int {
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

func (s *tsComments) regexp(i int) int {
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

func (s *tsComments) lineEnd(i int) int {
	if end := strings.IndexByte(s.data[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(s.data)
}

func identifierStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func tagNameByte(c byte) bool {
	return identifierStart(c) || c >= '0' && c <= '9' || c == '.' || c == ':' || c == '-' || c >= 128
}

// element scans the JSX element that starts at the '<' of data[i] and returns
// the offset after it, or -1 when the text is not a JSX element (a generic
// arrow such as <T,>(x: T) => x). A failed attempt leaves nothing recorded.
func (s *tsComments) element(i int) int {
	mark, elements := len(s.trail), len(*s.elements)
	end := s.jsx(i)
	if end < 0 {
		for _, offset := range s.trail[mark:] {
			delete(s.comments, offset)
		}
		s.trail = s.trail[:mark]
		*s.elements = (*s.elements)[:elements]
		return -1
	}
	*s.elements = append(*s.elements, span{i, end})
	return end
}

func (s *tsComments) skipSpace(i int) int {
	for i < len(s.data) {
		switch {
		case strings.ContainsRune(" \t\r\n", rune(s.data[i])):
			i++
		case strings.HasPrefix(s.data[i:], "//"):
			s.comment(i)
			i = s.lineEnd(i)
		case strings.HasPrefix(s.data[i:], "/*"):
			end := strings.Index(s.data[i+2:], "*/")
			if end < 0 {
				return len(s.data)
			}
			i += end + 4
		default:
			return i
		}
	}
	return i
}

func (s *tsComments) jsx(i int) int {
	i++
	start := i
	for i < len(s.data) && tagNameByte(s.data[i]) {
		i++
	}
	name := s.data[start:i]
	if name == "" && (i >= len(s.data) || s.data[i] != '>') {
		return -1
	}
	if name != "" && i < len(s.data) && s.data[i] == '<' {
		// Type arguments of a generic component: <Select<V> {...props} />.
		depth := 0
		for ; i < len(s.data); i++ {
			switch {
			case strings.HasPrefix(s.data[i:], "=>"):
				i++
			case s.data[i] == '<':
				depth++
			case s.data[i] == '>':
				depth--
			}
			if depth == 0 {
				i++
				break
			}
		}
	}
	// <T,>(x) and <T extends U>(x) are generic arrows, never elements.
	if i < len(s.data) && s.data[i] == ',' || strings.HasPrefix(strings.TrimLeft(s.data[i:], " \t"), "extends ") {
		return -1
	}
	for {
		i = s.skipSpace(i)
		if i >= len(s.data) {
			return -1
		}
		switch c := s.data[i]; {
		case c == '/' && i+1 < len(s.data) && s.data[i+1] == '>':
			return i + 2
		case c == '>':
			return s.children(i+1, name)
		case c == '{':
			i = s.code(i+1, 1)
		case tagNameByte(c):
			for i < len(s.data) && tagNameByte(s.data[i]) {
				i++
			}
			if j := s.skipSpace(i); j < len(s.data) && s.data[j] == '=' {
				i = s.skipSpace(j + 1)
				if i >= len(s.data) {
					return -1
				}
				switch s.data[i] {
				case '"', '\'':
					end := strings.IndexByte(s.data[i+1:], s.data[i])
					if end < 0 {
						return -1
					}
					i += end + 2
				case '{':
					i = s.code(i+1, 1)
				case '<':
					if i = s.element(i); i < 0 {
						return -1
					}
				default:
					return -1
				}
			}
		default:
			return -1
		}
	}
}

func (s *tsComments) children(i int, name string) int {
	for i < len(s.data) {
		switch {
		case s.data[i] == '{':
			i = s.code(i+1, 1)
		case strings.HasPrefix(s.data[i:], "</"):
			end := strings.IndexByte(s.data[i:], '>')
			if end < 0 || strings.TrimSpace(s.data[i+2:i+end]) != name {
				return -1
			}
			return i + end + 1
		case s.data[i] == '<':
			if i = s.element(i); i < 0 {
				return -1
			}
		default:
			i++
		}
	}
	return -1
}
