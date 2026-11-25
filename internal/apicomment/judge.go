package apicomment

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
)

const (
	problemMissing   = "has no doc comment"
	problemMulti     = "is declared with other names on one line, so one comment cannot describe it"
	problemName      = "has a doc comment that does not start with its name"
	problemRestating = "has a doc comment that only restates its name"
)

// judge returns the problem with a declaration's comment, or "" when its doc
// comment or trailing line comment is acceptable. A comment is acceptable when
// the declaration has a name of its own, the comment starts with that bare name
// and the whole comment says something beyond the name and the declaration's
// type (see substantive). typ is the declaration's type.
func judge(s site, name string, typ types.Type) string {
	if s.multi {
		return problemMulti
	}
	problem := problemMissing
	for _, group := range []*ast.CommentGroup{s.doc, s.trail} {
		if group == nil {
			continue
		}
		text := strings.Join(strings.Fields(group.Text()), " ")
		if text == "" {
			continue
		}
		reason := judgeText(text, name, typeWords(typ))
		if reason == "" {
			return ""
		}
		if problem == problemMissing {
			problem = reason
		}
	}
	return problem
}

func judgeText(text, name string, typeNames []string) string {
	rest, ok := strings.CutPrefix(text, name)
	if !ok || (rest != "" && isNameRune(firstRune(rest))) {
		return problemName
	}
	if !substantive(rest, name, typeNames) {
		return problemRestating
	}
	return ""
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return ' '
}

func isNameRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// linkingWords are the articles and linking words that add nothing when they
// surround the name's own words.
var linkingWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "of": true, "for": true,
	"to": true, "this": true, "that": true, "its": true, "it": true, "and": true, "or": true,
	"in": true, "on": true,
}

// fillerWords are the words that say nothing about a declaration whatever its
// name, compared without a plural "s". The guide lists them
// (developer-tooling-and-testing.md, "Comment the framework's caller inputs").
var fillerWords = map[string]bool{
	"value": true, "setting": true, "field": true, "option": true, "parameter": true, "flag": true, "todo": true,
}

// substantive reports whether the comment text after the name says something
// beyond it: once the articles and linking words, the filler words, the words of
// the name itself and the words of the declaration's type are removed (compared
// without a plural "s"), a word must remain anywhere in the comment. So
// "Timeout is the timeout" and "Name is a string" fail and "Timeout is how long a
// request may run" and "Timeout is the timeout. Zero means no limit." pass. This is
// a floor that catches empty comments, not a judgement of truth.
func substantive(text, name string, typeNames []string) bool {
	own := make(map[string]bool)
	for _, word := range nameWords(name) {
		own[stem(word)] = true
	}
	own[stem(strings.ToLower(name))] = true
	for _, word := range typeNames {
		own[stem(word)] = true
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if linkingWords[word] || fillerWords[stem(word)] || own[stem(word)] {
			continue
		}
		return true
	}
	return false
}

// typeWords are the words of the names in a declaration's type, so that
// "Name is a string" and "Timeout is the timeout duration" say nothing.
func typeWords(t types.Type) []string {
	if t == nil {
		return nil
	}
	text := types.TypeString(t, func(*types.Package) string { return "" })
	var words []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) {
		words = append(words, nameWords(part)...)
	}
	return words
}

func stem(word string) string {
	if len(word) > 3 {
		return strings.TrimSuffix(word, "s")
	}
	return word
}

// nameWords splits a Go name at lower-to-upper transitions and at the end of an
// acronym ("HTTPStatus" is "http", "status").
func nameWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		boundary := unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i]) ||
			unicode.IsDigit(runes[i-1]) != unicode.IsDigit(runes[i]) ||
			unicode.IsUpper(runes[i-1]) && unicode.IsUpper(runes[i]) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if boundary {
			words = append(words, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	return append(words, strings.ToLower(string(runes[start:])))
}
