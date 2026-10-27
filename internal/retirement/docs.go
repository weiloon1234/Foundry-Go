package retirement

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"strings"
)

var codeSpan = regexp.MustCompile("`+([^`]+)`+")

var beforeMarker = regexp.MustCompile(`^<!-- orm-example: [a-z0-9-]+-before -->$`)

// CheckDocument rejects retired spellings in active documentation. Only labelled,
// fenced before examples in the migration guide describe the removed API.
// Unqualified top-level names in code must use a retained, qualified spelling.
// Unqualified member spellings also require their retained owner where a type
// name is shared (for example orm.Query.OrderBy).
func CheckDocument(name string, data []byte) error {
	denied, err := declarations()
	if err != nil {
		return err
	}
	patterns := []*regexp.Regexp{regexp.MustCompile(`database/query\b`), regexp.MustCompile(`\bquery\.[A-Z]\w*`), regexp.MustCompile(`\bLegacy[A-Z]\w*`)}
	unqualified := map[string]bool{}
	for owner, names := range denied {
		for _, member := range names {
			unqualified[member] = true
			unqualified[path.Base(owner)+"."+member] = true
			name := member
			patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(path.Base(owner)+"."+name)+`\b`))
		}
	}
	var failures []error
	pending, before := false, false
	fence, language := "", ""
	var code []string
	codeLine := 0
	checkCode := func() {
		if !before && (language == "" || language == "go" || language == "golang") && retiredCodeName(strings.Join(code, "\n"), unqualified) {
			failures = append(failures, fmt.Errorf("%s:%d: unqualified retired API in Go block; use its retained qualified owner", name, codeLine))
		}
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if fence == "" && beforeMarker.MatchString(trimmed) {
			pending = name == "docs/guides/orm-migration.md"
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
				code = nil
				codeLine = i + 2
				language = strings.TrimSpace(strings.TrimLeft(trimmed, string(marker[0])))
				before = pending
				pending = false
			} else if fence == marker {
				checkCode()
				fence = ""
				before = false
			}
			continue
		}
		if before {
			continue
		}
		pending = false
		bad := false
		if fence != "" && (language == "" || language == "go" || language == "golang") {
			code = append(code, line)
		} else if fence == "" {
			for _, span := range codeSpan.FindAllStringSubmatch(line, -1) {
				bad = bad || retiredCodeName(span[1], unqualified)
			}
		}
		if bad {
			failures = append(failures, fmt.Errorf("%s:%d: unqualified retired API in code; use its retained qualified owner", name, i+1))
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				failures = append(failures, fmt.Errorf("%s:%d: retired API outside a migration before block", name, i+1))
				break
			}
		}
	}
	if fence != "" {
		checkCode()
	}
	return errors.Join(failures...)
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
