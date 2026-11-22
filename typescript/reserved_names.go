package typescript

import (
	"bytes"
	"regexp"
	"slices"
	"sync"
)

// renderedNames are module-level declarations the renderer itself emits;
// TestRenderedNamesListTheRendererDeclarations keeps the list complete.
var renderedNames = []string{
	"API", "CatalogMessageArguments", "CatalogMessageKey", "Client", "ContractTypes",
	"ErrorCode", "ErrorResponse", "FieldMessageKey", "FrontendMessageArguments", "FrontendMessageKey",
	"Identity", "Locale", "LocaleMap", "isOperationError",
	"OperationBodyLabels", "OperationErrorVariants", "OperationInputs", "OperationJSONBodies", "Operations",
	"PasswordCredentials", "PresentationKind", "PublicEnv", "PublicEnvEntry", "PublicEnvKey",
	"Realtime", "ReceivedContractTypes",
	"TableColumn", "TableColumnKind", "TableMetadata", "TableRows", "TableReceivedRows", "TablePageReceived", "Tables",
	"QueryTableId", "ExportTableId", "DownloadTableId", "TableQueryInputs", "TableQueryOutputs", "TableQueryOperationNames", "TableExportInputs", "TableExportOutputs", "TableExportOperationNames", "TableExportKinds", "TableDownloadInputs", "TableDownloadOutputs", "TableDownloadOperationNames", "TableQueries", "TableExports", "TableDownloads", "TableId", "TableReceivedRow", "TableRow", "TableSort",
}

// renderedGlobals are global types the renderer's own output names that the
// runtime code does not.
var renderedGlobals = []string{"ReadonlyArray"}

var (
	identifierToken = regexp.MustCompile(`[A-Za-z_$][\w$]*`)
	// A generic parameter list opens after a declared function, class,
	// interface or type name, a member signature at the start of a line, or
	// as an arrow function's own list after "=".
	genericList  = regexp.MustCompile(`(?m)(?:\b(?:function\s*\*?\s*[\w$]*|class\s+[\w$]+|interface\s+[\w$]+|type\s+[\w$]+)|^[ \t]*(?:(?:readonly|private|protected|public|static|async|get|set)\s+)*[\w$]+|=)\s*<`)
	inferredName = regexp.MustCompile(`\binfer\s+([\w$]+)`)
	mappedKey    = regexp.MustCompile(`\[\s*([\w$]+)\s+in\b`)
	localName    = regexp.MustCompile(`\b(?:const|let|var|function\s*\*?|class|interface|type|enum)\s+([\w$]+)`)
)

// reservedNames holds the capitalized names a consumer schema cannot take in a
// generated module (the rule is stated in standard_globals.go): the
// renderer's declarations, every runtime declaration at module scope, the
// global names the runtime and renderer code refer to, and the standard
// global type names. Names are read from code only; comments, string,
// template and regular-expression text, property names and type parameters
// are not names a schema declaration could collide with.
var reservedNames = sync.OnceValues(func() (map[string]bool, error) {
	result := make(map[string]bool)
	for _, name := range slices.Concat(renderedNames, renderedGlobals, standardGlobalNames) {
		result[name] = true
	}
	entries, err := runtimeSources.ReadDir("runtime")
	if err != nil {
		return nil, err
	}
	declared, referenced := make(map[string]bool), make(map[string]bool)
	for _, entry := range entries {
		data, err := runtimeSources.ReadFile("runtime/" + entry.Name())
		if err != nil {
			return nil, err
		}
		code := codeText(data)
		for _, match := range declaration.FindAllSubmatch(code, -1) {
			if capitalized(match[4]) {
				result[string(match[4])] = true
			}
		}
		for name := range localNames(code) {
			declared[name] = true
		}
		for name := range referencedNames(code) {
			referenced[name] = true
		}
	}
	for name := range referenced {
		if !declared[name] {
			result[name] = true
		}
	}
	return result, nil
})

func capitalized(name []byte) bool {
	return len(name) != 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// localNames yields every name the code declares at any scope: variables,
// functions, classes and types, generic parameters, inferred types and mapped
// type keys.
func localNames(code []byte) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, pattern := range []*regexp.Regexp{localName, inferredName, mappedKey} {
			for _, match := range pattern.FindAllSubmatch(code, -1) {
				if !yield(string(match[1])) {
					return
				}
			}
		}
		for _, bounds := range genericList.FindAllIndex(code, -1) {
			for _, parameter := range genericParameters(code, bounds[1]) {
				if !yield(parameter) {
					return
				}
			}
		}
	}
}

// genericParameters returns the parameter names of the generic list whose
// "<" ends at open: the first identifier of each top-level element.
func genericParameters(code []byte, open int) []string {
	var names []string
	depth, start := 0, open
	for i := open; i < len(code); i++ {
		switch c := code[i]; {
		case c == '<' || c == '(' || c == '[' || c == '{':
			depth++
		case c == '>' && code[i-1] == '=':
			// The arrow of a function type inside a constraint closes nothing.
		case c == ')' || c == ']' || c == '}' || c == '>':
			if depth == 0 {
				return append(names, firstIdentifier(code[start:i]))
			}
			depth--
		case c == ',' && depth == 0:
			names = append(names, firstIdentifier(code[start:i]))
			start = i + 1
		}
	}
	return names
}

func firstIdentifier(text []byte) string {
	return string(identifierToken.Find(text))
}

// referencedNames yields the capitalized identifiers the code refers to:
// neither a property reached with "." nor an object or type member's key.
func referencedNames(code []byte) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, bounds := range identifierToken.FindAllIndex(code, -1) {
			name := code[bounds[0]:bounds[1]]
			// A token after a digit is a numeric literal's suffix.
			if !capitalized(name) || bounds[0] > 0 && isIdentifierByte(code[bounds[0]-1]) {
				continue
			}
			before := bytes.TrimRight(code[:bounds[0]], " \t\r\n")
			if bytes.HasSuffix(before, []byte(".")) && !bytes.HasSuffix(before, []byte("...")) {
				continue
			}
			if memberKey(code, bounds[0], bounds[1], before) {
				continue
			}
			if !yield(string(name)) {
				return
			}
		}
	}
}

// memberKey reports a name followed by ":" or "?:" that starts an object or
// type member: after "{", ",", ";" or "readonly", or first on its line.
func memberKey(code []byte, start, end int, before []byte) bool {
	after := bytes.TrimLeft(code[end:], " \t")
	if !bytes.HasPrefix(after, []byte(":")) && !bytes.HasPrefix(after, []byte("?:")) {
		return false
	}
	line := code[bytes.LastIndexByte(code[:start], '\n')+1 : start]
	if len(bytes.TrimLeft(line, " \t")) == 0 || bytes.HasSuffix(before, []byte("readonly")) {
		return true
	}
	return len(before) != 0 && bytes.IndexByte([]byte("{,;"), before[len(before)-1]) >= 0
}

func isIdentifierByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// codeText blanks comments, the text of strings and templates and regular
// expression literals to spaces, keeping line breaks and every template's
// ${...} expressions, so only code remains.
func codeText(source []byte) []byte {
	out := bytes.Clone(source)
	blank := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	var templates []int // the brace depth at each open ${
	depth, last, word := 0, byte(0), ""
	for i := 0; i < len(source); {
		c, next := source[i], byte(0)
		if i+1 < len(source) {
			next = source[i+1]
		}
		switch {
		case c == '/' && next == '/':
			end := len(source)
			if at := bytes.IndexByte(source[i:], '\n'); at >= 0 {
				end = i + at
			}
			blank(i, end)
			i = end
			continue
		case c == '/' && next == '*':
			end := len(source)
			if at := bytes.Index(source[i+2:], []byte("*/")); at >= 0 {
				end = i + 2 + at + 2
			}
			blank(i, end)
			i = end
			continue
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(source) && source[j] != c && source[j] != '\n' {
				if source[j] == '\\' {
					j++
				}
				j++
			}
			blank(i+1, j)
			i, last, word = j+1, c, ""
			continue
		case c == '`' || c == '}' && len(templates) != 0 && templates[len(templates)-1] == depth:
			if c == '}' {
				templates = templates[:len(templates)-1]
			}
			j := i + 1
			for j < len(source) && source[j] != '`' && (source[j] != '$' || j+1 >= len(source) || source[j+1] != '{') {
				if source[j] == '\\' {
					j++
				}
				j++
			}
			blank(i+1, j)
			if j < len(source) && source[j] == '$' {
				templates = append(templates, depth)
				i = j + 2
			} else {
				i = j + 1
			}
			last, word = '`', ""
			continue
		case c == '/' && regularExpressionAllowed(last, word):
			j, class := i+1, false
			for j < len(source) && source[j] != '\n' {
				if source[j] == '\\' {
					j += 2
					continue
				}
				if source[j] == '[' {
					class = true
				} else if source[j] == ']' {
					class = false
				} else if source[j] == '/' && !class {
					break
				}
				j++
			}
			j++
			for j < len(source) && isIdentifierByte(source[j]) {
				j++
			}
			blank(i, j)
			i, last, word = j, ')', ""
			continue
		case c == '{':
			depth++
		case c == '}':
			depth--
		}
		switch {
		case isIdentifierByte(c):
			if i == 0 || !isIdentifierByte(source[i-1]) {
				word = ""
			}
			word += string(c)
			last = c
		case c != ' ' && c != '\t' && c != '\r' && c != '\n':
			last, word = c, ""
		}
		i++
	}
	return out
}

// regularExpressionAllowed reports whether a "/" after the previous code
// character or keyword starts a regular expression rather than a division.
func regularExpressionAllowed(last byte, word string) bool {
	switch word {
	case "return", "typeof", "case", "in", "of", "delete", "void", "throw", "new", "else", "do", "yield", "await":
		return true
	}
	return last == 0 || bytes.IndexByte([]byte("(,=:[!&|?{};+-*%<>~^"), last) >= 0
}
