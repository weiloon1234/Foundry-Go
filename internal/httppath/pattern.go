// Package httppath shares HTTP pattern grammar between runtime and generation.
package httppath

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

//go:generate go run ./cmd/export

const parameterPrefix = "{"
const parameterSuffix = "}"
const tailSuffix = "..."
const parameterNamePattern = `[A-Za-z_][A-Za-z0-9_]*`
const forbiddenLiteralCharacters = "{}%?#\\"
const dotSegment = "."
const parentSegment = ".."
const controlMax = 31
const controlDelete = 127

var parameterName = regexp.MustCompile("^" + parameterNamePattern + "$")

// Syntax describes the parser's single-segment subset for deployment tooling.
// Deploy policies add resource bounds and exclude catch-all parameters.
func Syntax() Description {
	return Description{parameterPrefix, parameterSuffix, tailSuffix, parameterNamePattern,
		forbiddenLiteralCharacters, [2]string{dotSegment, parentSegment}, controlMax, controlDelete, "exact", true, false}
}

// Description is the machine-readable grammar exported with the deploy tools.
type Description struct {
	ParameterPrefix            string    `json:"parameter_prefix"`
	ParameterSuffix            string    `json:"parameter_suffix"`
	TailSuffix                 string    `json:"tail_suffix"`
	ParameterName              string    `json:"parameter_name"`
	ForbiddenLiteralCharacters string    `json:"forbidden_literal_characters"`
	ForbiddenSegments          [2]string `json:"forbidden_segments"`
	ControlMax                 int       `json:"control_max"`
	ControlDelete              int       `json:"control_delete"`
	TrailingSlash              string    `json:"trailing_slash"`
	UTF8Required               bool      `json:"utf8_required"`
	// Upload ingress bounds bytes; ValidText owns UTF-8 validation in the router.
	UploadIngressUTF8Enforced bool `json:"upload_ingress_utf8_enforced"`
}

// Segment is a literal or named parameter in the shared route grammar.
type Segment struct {
	Literal string
	Name    string
	Tail    bool
}

// Parse validates route syntax without binding a concrete DTO or starting I/O.
func Parse(pattern string) ([]Segment, error) {
	if pattern == "" || pattern[0] != '/' || !utf8.ValidString(pattern) {
		return nil, safeerror.NewFault(fault.Invalid, "route path must start with a slash")
	}
	parts := strings.Split(pattern[1:], "/")
	segments := make([]Segment, 0, len(parts))
	names := make(map[string]bool)
	for i, part := range parts {
		if part == "" && i != len(parts)-1 {
			return nil, safeerror.NewFault(fault.Invalid, "route path contains an empty segment")
		}
		if strings.HasPrefix(part, parameterPrefix) && strings.HasSuffix(part, parameterSuffix) {
			name := part[1 : len(part)-1]
			tail := strings.HasSuffix(name, tailSuffix)
			if tail {
				name = strings.TrimSuffix(name, tailSuffix)
			}
			if !validPathName(name) || names[name] || tail && i != len(parts)-1 {
				return nil, safeerror.NewFault(fault.Invalid, "route path has an invalid or duplicate parameter")
			}
			names[name] = true
			segments = append(segments, Segment{Name: name, Tail: tail})
			continue
		}
		if part == dotSegment || part == parentSegment || strings.ContainsAny(part, forbiddenLiteralCharacters) || !ValidText(part) {
			return nil, safeerror.NewFault(fault.Invalid, "route path contains an invalid literal segment")
		}
		segments = append(segments, Segment{Literal: part})
	}
	return segments, nil
}

func validPathName(name string) bool {
	return parameterName.MatchString(name)
}

// ValidText checks lossless UTF-8 and excludes control characters.
func ValidText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, ch := range text {
		if ch <= controlMax || ch == controlDelete {
			return false
		}
	}
	return true
}
