package retirement

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var codeSpan = regexp.MustCompile("`+([^`]+)`+")
var proseSelector = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*(\(\))?(\.[A-Za-z_][A-Za-z_0-9]*(\(\))?)+`)
var jsxElement = regexp.MustCompile(`</?[A-Za-z_$][A-Za-z_0-9$]*(\.[A-Za-z_$][A-Za-z_0-9$]*)*([ \t\r\n]+([A-Za-z_{]([^<>"']|"[^"]*"|'[^']*')*)?)?/?>`)

// CheckDocument rejects retired spellings in active documentation without historical exemptions.
func CheckDocument(name string, data []byte) error {
	return checkDocument(name, data, true)
}

// CheckApplicationDocument checks framework teaching in application instructions.
// Bare names alone do not establish framework ownership.
func CheckApplicationDocument(name string, data []byte) error {
	return checkDocument(name, data, false)
}

func checkDocument(name string, data []byte, strict bool) error {
	denied, err := declarations()
	if err != nil {
		return err
	}
	rules, err := loadDocumentRules(denied)
	if err != nil {
		return err
	}
	commands := make(map[*regexp.Regexp]string)
	for command, replacement := range rules.RetiredCommands {
		words := strings.Fields(strings.TrimPrefix(command, "foundry "))
		for i, word := range words {
			words[i] = regexp.QuoteMeta(word)
		}
		commands[regexp.MustCompile(`\b`+strings.Join(words, `[ \t]+`)+`\b`)] = replacement
	}
	patterns := []*regexp.Regexp{regexp.MustCompile(`database/query\b`), regexp.MustCompile(`\bquery\.[A-Z]\w*`)}
	for _, directive := range rules.RetiredDirectives {
		patterns = append(patterns, regexp.MustCompile(`foundry:`+regexp.QuoteMeta(directive)+`\b`))
	}
	if strict {
		patterns = append(patterns, regexp.MustCompile(`\bLegacy[A-Z]\w*`))
	}
	unqualified := map[string]bool{}
	for owner, names := range denied {
		for _, member := range names {
			if rules.owns(owner, member) {
				continue
			}
			if strict {
				unqualified[member] = true
			}
			unqualified[path.Base(owner)+"."+member] = true
			name := member
			patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(path.Base(owner)+"."+name)+`\b`))
		}
	}
	var failures []error
	fence, language := "", ""
	var code []string
	codeLine := 0
	checkCode := func() {
		if !javascriptFence(language) {
			text := withoutJSXElements(strings.Join(code, "\n"))
			failures = append(failures, rules.checkCode(name, text, codeLine, !strict)...)
			if spelling, relativeLine := retiredCodeName(text, unqualified); (language == "" || language == "go" || language == "golang") && spelling != "" {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: %s", name, codeLine+relativeLine-1, rules.retirementMessage(spelling))))
			}
		}
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
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
				continue
			} else if fence != "" && fence[0] == marker[0] && len(marker) >= len(fence) && strings.TrimSpace(trimmed[n:]) == "" {
				checkCode()
				fence = ""
				continue
			}
		}
		if fence != "" && javascriptFence(language) {
			continue
		}
		for pattern, replacement := range commands {
			if pattern.MatchString(line) {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: retired command; %s", name, i+1, replacement)))
			}
		}
		bad := ""
		if fence != "" {
			code = append(code, line)
		}
		line = withoutJSXElements(line)
		if fence == "" {
			for _, span := range codeSpan.FindAllStringSubmatch(line, -1) {
				if bad == "" {
					bad, _ = retiredCodeName(span[1], unqualified)
				}
				failures = append(failures, rules.checkCode(name, span[1], i+1, !strict)...)
			}
			prose := codeSpan.ReplaceAllString(line, "")
			for _, selector := range proseSelector.FindAllString(prose, -1) {
				failures = append(failures, rules.checkCode(name, selector, i+1, !strict)...)
			}
		}
		if bad != "" {
			failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: %s", name, i+1, rules.retirementMessage(bad))))
			continue
		}
		for _, pattern := range patterns {
			if spelling := pattern.FindString(line); spelling != "" {
				message := "retired API in teaching; no replacement is recorded for this spelling; remove it and consult the current task guide."
				if _, exists := rules.replacements[retirementName(spelling)]; exists {
					message = rules.retirementMessage(spelling)
				}
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: %s", name, i+1, message)))
				break
			}
		}
	}
	if fence != "" {
		checkCode()
	}
	return safeerror.Join("operation failed", failures...)
}

func javascriptFence(language string) bool {
	fields := strings.Fields(strings.ToLower(language))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "js", "javascript", "jsx", "ts", "typescript", "tsx":
		return true
	}
	return false
}

func withoutJSXElements(text string) string {
	var masked []byte
	for _, match := range jsxElement.FindAllStringIndex(text, -1) {
		if match[0] > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:match[0]])
			// Name<Placeholder> is documentation notation, not a JSX tag.
			if previous == '_' || previous == '$' || unicode.IsLetter(previous) || unicode.IsDigit(previous) {
				continue
			}
		}
		if masked == nil {
			masked = []byte(text)
		}
		for i := match[0]; i < match[1]; i++ {
			if masked[i] != '\n' {
				masked[i] = ' '
			}
		}
	}
	if masked == nil {
		return text
	}
	return string(masked)
}

// Scan code rather than prose or string contents. A selector rooted at another
// package/type is not an unqualified reference to a removed package declaration.
func retiredCodeName(code string, denied map[string]bool) (string, int) {
	var scan scanner.Scanner
	fs := token.NewFileSet()
	scan.Init(fs.AddFile("", -1, len(code)), []byte(code), func(token.Position, string) {}, 0)
	type lexeme struct {
		kind token.Token
		name string
		line int
	}
	var tokens []lexeme
	for {
		pos, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		tokens = append(tokens, lexeme{kind, literal, fs.Position(pos).Line})
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
			return item.name, item.line
		}
		if end+1 < len(tokens) && tokens[end].kind == token.PERIOD && tokens[end+1].kind == token.IDENT && denied[qualifier+item.name+"."+tokens[end+1].name] {
			return qualifier + item.name + "." + tokens[end+1].name, item.line
		}
	}
	return "", 0
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
