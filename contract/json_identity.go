package contract

import (
	"go/token"
	"strings"
	"unicode"
)

// Go reports an executable's package as main, including named generic arguments.
// Compare the structure and every imported name exactly. Only a main.X token may
// use a generated source package; runtime reflection cannot recover that path.
// This is declaration metadata, never model identity or authentication authority.
func mainTypeIdentityMatches(runtime, source string) bool {
	if runtime == source {
		return true
	}
	for {
		ri, si := strings.IndexAny(runtime, "[],"), strings.IndexAny(source, "[],")
		if (ri < 0) != (si < 0) {
			return false
		}
		if ri < 0 {
			return mainTypeNameMatches(runtime, source)
		}
		if runtime[ri] != source[si] || !mainTypeNameMatches(runtime[:ri], source[:si]) {
			return false
		}
		runtime, source = runtime[ri+1:], source[si+1:]
	}
}
func mainTypeNameMatches(runtime, source string) bool {
	if runtime == source {
		return true
	}
	name, ok := strings.CutPrefix(runtime, "main.")
	if !ok || !token.IsIdentifier(name) {
		return false
	}
	namespace, ok := strings.CutSuffix(source, "."+name)
	return ok && declarationText(namespace) && !strings.ContainsAny(namespace, "\\[](){}*,;\"`") && !strings.ContainsFunc(namespace, unicode.IsSpace)
}
