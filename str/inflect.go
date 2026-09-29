package str

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// irregularPlurals pairs singular and plural English nouns that regular
// suffix rules would inflect incorrectly.
var irregularPlurals = [...][2]string{
	{"alias", "aliases"}, {"alumnus", "alumni"}, {"analysis", "analyses"}, {"appendix", "appendices"},
	{"atlas", "atlases"}, {"axis", "axes"}, {"bacterium", "bacteria"}, {"basis", "bases"},
	{"bus", "buses"}, {"cactus", "cacti"}, {"calf", "calves"}, {"campus", "campuses"},
	{"canvas", "canvases"}, {"child", "children"}, {"cookie", "cookies"}, {"crisis", "crises"},
	{"criterion", "criteria"}, {"curriculum", "curricula"}, {"diagnosis", "diagnoses"}, {"die", "dice"},
	{"echo", "echoes"}, {"elf", "elves"}, {"focus", "foci"}, {"foot", "feet"},
	{"fungus", "fungi"}, {"gas", "gases"}, {"genus", "genera"}, {"goose", "geese"},
	{"half", "halves"}, {"hero", "heroes"}, {"index", "indices"}, {"knife", "knives"},
	{"leaf", "leaves"}, {"lens", "lenses"}, {"life", "lives"}, {"loaf", "loaves"},
	{"man", "men"}, {"matrix", "matrices"}, {"medium", "media"}, {"mouse", "mice"},
	{"movie", "movies"}, {"nucleus", "nuclei"}, {"octopus", "octopuses"}, {"ox", "oxen"},
	{"person", "people"}, {"phenomenon", "phenomena"}, {"potato", "potatoes"}, {"quiz", "quizzes"},
	{"radius", "radii"}, {"self", "selves"}, {"shelf", "shelves"}, {"status", "statuses"},
	{"stimulus", "stimuli"}, {"syllabus", "syllabi"}, {"thesis", "theses"}, {"thief", "thieves"},
	{"tomato", "tomatoes"}, {"tooth", "teeth"}, {"torpedo", "torpedoes"}, {"vertex", "vertices"},
	{"veto", "vetoes"}, {"virus", "viruses"}, {"wife", "wives"}, {"wolf", "wolves"},
	{"woman", "women"}, {"zombie", "zombies"},
}

// uncountable nouns keep one form.
var uncountable = [...]string{
	"advice", "aircraft", "audio", "bison", "cattle", "chassis", "data", "deer", "equipment",
	"evidence", "feedback", "fish", "furniture", "homework", "information", "knowledge", "luggage",
	"metadata", "moose", "money", "music", "news", "offspring", "police", "rice", "salmon", "series",
	"sheep", "software", "species", "staff", "swine", "traffic", "trout",
}

// Plural returns the English plural of the last word of text, preserving the
// rest of text and the word's capitalization: "blog post" becomes "blog posts"
// and "Person" becomes "People". Words already in an irregular plural form,
// uncountable nouns and words ending in a single "s" are returned unchanged.
func Plural(text string) string {
	prefix, word := lastWord(text)
	lower := strings.ToLower(word)
	if word == "" || isUncountable(lower) {
		return text
	}
	for _, pair := range irregularPlurals {
		switch lower {
		case pair[0]:
			return prefix + wordCase(word, pair[1])
		case pair[1]:
			return text
		}
	}
	switch {
	case strings.HasSuffix(lower, "ss") || hasAnySuffix(lower, "x", "z", "ch", "sh"):
		return prefix + word + suffixCase(word, "es")
	case strings.HasSuffix(lower, "s"):
		return text
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && (!isVowel(lower[len(lower)-2]) || strings.HasSuffix(lower, "quy")):
		return prefix + word[:len(word)-1] + suffixCase(word, "ies")
	}
	return prefix + word + suffixCase(word, "s")
}

// Singular returns the English singular of the last word of text, preserving
// the rest of text and the word's capitalization: "Categories" becomes
// "Category". Words that already look singular are returned unchanged.
func Singular(text string) string {
	prefix, word := lastWord(text)
	lower := strings.ToLower(word)
	if word == "" || isUncountable(lower) {
		return text
	}
	for _, pair := range irregularPlurals {
		switch lower {
		case pair[1]:
			return prefix + wordCase(word, pair[0])
		case pair[0]:
			return text
		}
	}
	switch {
	case strings.HasSuffix(lower, "ies") && len(lower) > 4:
		return prefix + word[:len(word)-3] + suffixCase(word, "y")
	case hasAnySuffix(lower, "sses", "xes", "zzes", "ches", "shes"):
		return prefix + word[:len(word)-2]
	case hasAnySuffix(lower, "ss", "us", "is"):
		return text
	case strings.HasSuffix(lower, "s") && len(lower) > 1:
		return prefix + word[:len(word)-1]
	}
	return text
}

// Pluralize returns text for a count of 1 or -1 and Plural(text) otherwise,
// for labels such as "1 item" and "3 items".
func Pluralize(text string, count int) string {
	if count == 1 || count == -1 {
		return text
	}
	return Plural(text)
}

// lastWord splits text after its last space, hyphen or underscore.
func lastWord(text string) (string, string) {
	index := strings.LastIndexFunc(text, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '_' })
	if index < 0 {
		return "", text
	}
	_, size := utf8.DecodeRuneInString(text[index:])
	return text[:index+size], text[index+size:]
}

// wordCase applies the capitalization of word to a lowercase replacement word:
// all capitals stay capitals and an initial capital is kept.
func wordCase(word, replacement string) string {
	if allCapitals(word) {
		return strings.ToUpper(replacement)
	}
	if first, _ := utf8.DecodeRuneInString(word); unicode.IsUpper(first) {
		r, size := utf8.DecodeRuneInString(replacement)
		return string(unicode.ToUpper(r)) + replacement[size:]
	}
	return replacement
}

// suffixCase capitalizes a lowercase suffix appended to an all-capitals word.
func suffixCase(word, suffix string) string {
	if allCapitals(word) {
		return strings.ToUpper(suffix)
	}
	return suffix
}

func allCapitals(word string) bool {
	return utf8.RuneCountInString(word) > 1 && word == strings.ToUpper(word) && word != strings.ToLower(word)
}

func isUncountable(word string) bool {
	for _, item := range uncountable {
		if item == word {
			return true
		}
	}
	return false
}

func hasAnySuffix(text string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(text, suffix) {
			return true
		}
	}
	return false
}

func isVowel(b byte) bool { return strings.IndexByte("aeiou", b) >= 0 }
