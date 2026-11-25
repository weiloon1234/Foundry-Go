package apicomment

import (
	"go/ast"
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
// the declaration has a name of its own and the comment's first sentence starts
// with that bare name and says something beyond it (see substantive).
func judge(s site, name string) string {
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
		reason := judgeText(text, name)
		if reason == "" {
			return ""
		}
		if problem == problemMissing {
			problem = reason
		}
	}
	return problem
}

func judgeText(text, name string) string {
	rest, ok := strings.CutPrefix(text, name)
	if !ok || (rest != "" && isNameRune(firstRune(rest))) {
		return problemName
	}
	if !substantive(firstSentence(rest), name) {
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

// firstSentence is text up to the first sentence end: a period, question mark
// or exclamation mark followed by a space or the end of the text.
func firstSentence(text string) string {
	for i, r := range text {
		if (r == '.' || r == '?' || r == '!') && (i+1 == len(text) || text[i+1] == ' ') {
			return text[:i]
		}
	}
	return text
}

// fillerWords are the articles and linking words that add nothing when they
// surround the name's own words.
var fillerWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "of": true, "for": true,
	"to": true, "this": true, "that": true, "its": true, "it": true, "and": true, "or": true,
	"in": true, "on": true,
}

// substantive reports whether the sentence after the name says something
// beyond it: once the articles and linking words and the words of the name
// itself (compared without a plural "s") are removed, a word must remain. So
// "Timeout is the timeout" fails and "Timeout is how long a request may run"
// passes.
func substantive(sentence, name string) bool {
	own := make(map[string]bool)
	for _, word := range nameWords(name) {
		own[stem(word)] = true
	}
	own[stem(strings.ToLower(name))] = true
	for _, word := range strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if fillerWords[word] || own[stem(word)] {
			continue
		}
		return true
	}
	return false
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
