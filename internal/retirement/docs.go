package retirement

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"strings"
)

var codeSpan = regexp.MustCompile("`+([^`]+)`+")
var proseSelector = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*(\(\))?(\.[A-Za-z_][A-Za-z_0-9]*(\(\))?)+`)

var beforeMarker = regexp.MustCompile(`^<!-- orm-example: [a-z0-9-]+-before -->$`)
var textBlockBoundary = regexp.MustCompile(`^(#{1,6}([ \t]|$)|>|<|[-=_*]{3,}[ \t]*$)`)
var listItemStart = regexp.MustCompile(`^([-+*][ \t]|[0-9]+[.)][ \t])`)

// CheckDocument rejects retired spellings in active documentation. Only labelled
// before blocks in the designated migration guides describe the removed API.
// A label covers the immediately following fence, table, or paragraph (including
// one list item), never a heading or an entire section.
// Unqualified top-level names in code must use a retained, qualified spelling.
// Unqualified member spellings also require their retained owner where a type
// name is shared (for example orm.Query.OrderBy).
func CheckDocument(name string, data []byte) error {
	denied, err := declarations()
	if err != nil {
		return err
	}
	rules, err := loadDocumentRules(denied)
	if err != nil {
		return err
	}
	patterns := []*regexp.Regexp{regexp.MustCompile(`database/query\b`), regexp.MustCompile(`\bquery\.[A-Z]\w*`), regexp.MustCompile(`\bLegacy[A-Z]\w*`)}
	unqualified := map[string]bool{}
	for owner, names := range denied {
		for _, member := range names {
			if rules.owns(owner, member) {
				continue
			}
			unqualified[member] = true
			unqualified[path.Base(owner)+"."+member] = true
			name := member
			patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(path.Base(owner)+"."+name)+`\b`))
		}
	}
	var failures []error
	pending, before := false, false
	textBefore, tableBefore := false, false
	fence, language := "", ""
	var code []string
	codeLine := 0
	checkCode := func() {
		if !before {
			text := strings.Join(code, "\n")
			failures = append(failures, rules.checkCode(name, text, codeLine)...)
			if (language == "" || language == "go" || language == "golang") && retiredCodeName(text, unqualified) {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: unqualified retired API in Go block; use its retained qualified owner", name, codeLine)))
			}
		}
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if fence == "" && beforeMarker.MatchString(trimmed) {
			pending = name == "docs/guides/orm-migration.md" || name == "docs/guides/v0.2-migration.md"
			textBefore = false
			if !pending {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: migration before labels are allowed only in designated migration guides", name, i+1)))
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			marker := trimmed[:n]
			if fence == "" && (marker[0] != '`' || !strings.Contains(trimmed[n:], "`")) {
				fence = marker
				code = nil
				codeLine = i + 2
				language = strings.TrimSpace(strings.TrimLeft(trimmed, string(marker[0])))
				before = pending
				pending = false
				textBefore = false
				continue
			} else if fence != "" && fence[0] == marker[0] && len(marker) >= len(fence) && strings.TrimSpace(trimmed[n:]) == "" {
				checkCode()
				fence = ""
				before = false
				continue
			}
		}
		if before {
			continue
		}
		if fence == "" {
			if pending {
				textBefore = trimmed != "" && !textBlockBoundary.MatchString(trimmed)
				tableBefore = strings.HasPrefix(trimmed, "|")
			} else if textBefore {
				textBefore = trimmed != "" && !textBlockBoundary.MatchString(trimmed) && !listItemStart.MatchString(trimmed) && tableBefore == strings.HasPrefix(trimmed, "|")
			}
		}
		pending = false
		if fence == "" && textBefore {
			continue
		}
		bad := false
		if fence != "" {
			code = append(code, line)
		} else if fence == "" {
			for _, span := range codeSpan.FindAllStringSubmatch(line, -1) {
				bad = bad || retiredCodeName(span[1], unqualified)
				failures = append(failures, rules.checkCode(name, span[1], i+1)...)
			}
			prose := codeSpan.ReplaceAllString(line, "")
			for _, selector := range proseSelector.FindAllString(prose, -1) {
				failures = append(failures, rules.checkCode(name, selector, i+1)...)
			}
			for helper, replacement := range rules.Helpers {
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(helper) + `\b`).MatchString(prose) {
					failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: retired documentation helper %s; use %s", name, i+1, helper, replacement)))
				}
			}
		}
		if bad {
			failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: unqualified retired API in code; use its retained qualified owner", name, i+1)))
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: retired API outside a migration before block", name, i+1)))
				break
			}
		}
	}
	if fence != "" {
		checkCode()
	}
	return safeerror.Join("operation failed", failures...)
}

// Scan code rather than prose or string contents. A selector rooted at another
// package/type is not an unqualified reference to a removed package declaration.
func retiredCodeName(code string, denied map[string]bool) bool {
	var scan scanner.Scanner
	fs := token.NewFileSet()
	scan.Init(fs.AddFile("", -1, len(code)), []byte(code), func(token.Position, string) {}, 0)
	type lexeme struct {
		kind token.Token
		name string
	}
	var tokens []lexeme
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		tokens = append(tokens, lexeme{kind, literal})
	}
	brackets := 0
	for i, item := range tokens {
		if item.kind == token.LBRACK {
			brackets++
		}
		if item.kind == token.RBRACK {
			brackets--
		}
		if item.kind != token.IDENT {
			continue
		}
		// A function or method declaration names its own symbol; that name is
		// not a use of a retired API that happens to share it.
		if declaredFunctionName(tokens[:i], func(t lexeme) token.Token { return t.kind }) {
			continue
		}
		qualifier := ""
		if i > 0 && tokens[i-1].kind == token.PERIOD {
			if i < 2 || tokens[i-2].kind != token.IDENT {
				continue
			}
			qualifier = tokens[i-2].name + "."
		}
		// Bare API names and callable/function-value references are meaningful;
		// a field name, type placeholder or selector root is not that API.
		end := i + 1
		if end < len(tokens) && tokens[end].kind == token.LBRACK {
			depth := 0
			for ; end < len(tokens); end++ {
				if tokens[end].kind == token.LBRACK {
					depth++
				}
				if tokens[end].kind == token.RBRACK {
					depth--
					if depth == 0 {
						end++
						break
					}
				}
			}
		}
		bare := i == 0 && (len(tokens) == 1 || len(tokens) == 2 && tokens[1].kind == token.SEMICOLON)
		call := end < len(tokens) && tokens[end].kind == token.LPAREN && (i == 0 || tokens[i-1].kind != token.RPAREN)
		assigned := i > 0 && (tokens[i-1].kind == token.ASSIGN || tokens[i-1].kind == token.DEFINE) && (i+1 == len(tokens) || tokens[i+1].kind != token.PERIOD)
		previous, next := token.ILLEGAL, token.EOF
		if i > 0 {
			previous = tokens[i-1].kind
		}
		if i+1 < len(tokens) {
			next = tokens[i+1].kind
		}
		reference := brackets == 0 && previous != token.IDENT && previous != token.RPAREN && next != token.IDENT && next != token.PERIOD && next != token.COLON && next != token.LSS
		if qualifier == "" && denied[item.name] && (bare || call || assigned || reference) {
			return true
		}
		if end+1 < len(tokens) && tokens[end].kind == token.PERIOD && tokens[end+1].kind == token.IDENT && denied[qualifier+item.name+"."+tokens[end+1].name] {
			return true
		}
	}
	return false
}

// declaredFunctionName reports whether the identifier after prefix is the name
// in a function declaration ("func Name") or a method declaration
// ("func (receiver) Name").
func declaredFunctionName[T any](prefix []T, kind func(T) token.Token) bool {
	n := len(prefix)
	if n == 0 {
		return false
	}
	if kind(prefix[n-1]) == token.FUNC {
		return true
	}
	if kind(prefix[n-1]) != token.RPAREN {
		return false
	}
	depth := 0
	for i := n - 1; i >= 0; i-- {
		switch kind(prefix[i]) {
		case token.RPAREN:
			depth++
		case token.LPAREN:
			depth--
			if depth == 0 {
				return i > 0 && kind(prefix[i-1]) == token.FUNC
			}
		}
	}
	return false
}
