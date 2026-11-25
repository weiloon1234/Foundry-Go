package retirement

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/scanner"
	"go/token"
	"golang.org/x/net/html"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var proseSelector = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*(\(\))?(\.[A-Za-z_][A-Za-z_0-9]*(\(\))?)+`)

// CheckDocument rejects retired spellings in active documentation without historical exemptions.
func CheckDocument(name string, data []byte) error {
	checker, err := NewDocumentChecker(true)
	if err != nil {
		return err
	}
	return checker.Check(name, data)
}

// CheckApplicationDocument checks framework teaching in application instructions.
// Bare names alone do not establish framework ownership.
func CheckApplicationDocument(name string, data []byte) error {
	checker, err := NewDocumentChecker(false)
	if err != nil {
		return err
	}
	return checker.Check(name, data)
}

// DocumentChecker compiles immutable retirement rules once for a document run.
type DocumentChecker struct {
	rules       documentRules
	commands    map[*regexp.Regexp]string
	patterns    []*regexp.Regexp
	unqualified map[string]bool
	strict      bool
}

// NewDocumentChecker snapshots the ledger and compiles rules for framework or application teaching.
func NewDocumentChecker(strict bool) (*DocumentChecker, error) {
	denied, err := declarations()
	if err != nil {
		return nil, err
	}
	rules, err := loadDocumentRules(denied)
	if err != nil {
		return nil, err
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
	return &DocumentChecker{rules: rules, commands: commands, patterns: patterns, unqualified: unqualified, strict: strict}, nil
}

// Check retains import ownership across each complete code block.
func (c *DocumentChecker) Check(name string, data []byte) error {
	if len(data) > markdown.MaxDocumentBytes {
		return safeerror.New(fmt.Sprintf("%s:0: document exceeds maximum %d bytes.", name, markdown.MaxDocumentBytes))
	}
	parsed := markdown.Read(data)
	var failures []error
	report := func(line int, message string) {
		failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: %s", name, line, message)))
	}
	checkText := func(text string, line int) {
		for i, value := range strings.Split(text, "\n") {
			for pattern, replacement := range c.commands {
				if pattern.MatchString(value) {
					report(line+i, "retired command; "+replacement)
				}
			}
			for _, pattern := range c.patterns {
				if spelling := pattern.FindString(value); spelling != "" {
					message := "retired API in teaching; no replacement is recorded for this spelling; remove it and consult the current task guide."
					if _, exists := c.rules.replacements[retirementName(spelling)]; exists {
						message = c.rules.retirementMessage(spelling)
					}
					report(line+i, message)
					break
				}
			}
		}
	}
	for _, block := range parsed.Blocks {
		if block.Kind == markdown.HTML {
			markdown.HTMLTokens(block, func(kind html.TokenType, raw string, line int) {
				if kind != html.TextToken {
					return
				}
				checkText(raw, line)
				for _, selector := range proseSelector.FindAllString(raw, -1) {
					failures = append(failures, c.rules.checkCode(name, selector, line, !c.strict)...)
				}
			})
		}
		if block.Kind != markdown.Fence && block.Kind != markdown.Indented {
			continue
		}
		if javascriptFence(block.Info) {
			continue
		}
		line := block.First
		if block.Kind == markdown.Fence {
			line++
		}
		code := withoutJSXElements(block.Text)
		failures = append(failures, c.rules.checkCode(name, code, line, !c.strict)...)
		if spelling, relative := retiredCodeName(code, c.unqualified); (block.Info == "" || block.Info == "go" || block.Info == "golang") && spelling != "" {
			report(line+relative-1, c.rules.retirementMessage(spelling))
		}
		checkText(code, line)
	}
	for _, node := range parsed.Inlines {
		switch node.Kind {
		case markdown.CodeSpan:
			node.Text = withoutJSXElements(node.Text)
			failures = append(failures, c.rules.checkCode(name, node.Text, node.First, !c.strict)...)
			if spelling, _ := retiredCodeName(node.Text, c.unqualified); spelling != "" {
				report(node.First, c.rules.retirementMessage(spelling))
			}
			checkText(node.Text, node.First)
		case markdown.Text:
			for _, selector := range proseSelector.FindAllString(node.Text, -1) {
				failures = append(failures, c.rules.checkCode(name, selector, node.First, !c.strict)...)
			}
			checkText(node.Text, node.First)
		}
	}
	return safeerror.Join("documentation retirement check failed", failures...)
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
	offset := 0
	markdown.HTMLTokens(markdown.Node{Text: text, First: 1}, func(kind html.TokenType, raw string, _ int) {
		start := offset
		offset += len(raw)
		if kind != html.StartTagToken && kind != html.EndTagToken && kind != html.SelfClosingTagToken {
			return
		}
		// HTML tokenization is intentionally permissive. JSX requires an attribute
		// name, so a Go comparison such as <count && rows.Count(items) > is not JSX.
		tokenizer := html.NewTokenizer(strings.NewReader(raw))
		tokenizer.Next()
		element := tokenizer.Token()
		if len(element.Attr) > 0 {
			first, _ := utf8.DecodeRuneInString(element.Attr[0].Key)
			if first != '_' && first != '{' && !unicode.IsLetter(first) {
				return
			}
		}
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			if previous == '_' || previous == '$' || unicode.IsLetter(previous) || unicode.IsDigit(previous) {
				return
			}
		}
		if masked == nil {
			masked = []byte(text)
		}
		for i := start; i < offset; i++ {
			if masked[i] != '\n' {
				masked[i] = ' '
			}
		}
	})
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
