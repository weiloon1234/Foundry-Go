package generate

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
)

// transportField owns a concrete struct field, source and wire name. Query,
// header and multipart discovery share tag parsing, visibility and collisions.
type transportField struct {
	name, parameter string
	source          string
	typ             types.Type
	position        token.Pos
	index           int
	options         map[string]bool
	presentation    contract.Presentation
}

func discoverTransportFields(p *packageInput, structure *types.Struct, tag string, allowed map[string]bool, forbidden []string) ([]transportField, error) {
	var fields []transportField
	seen := make(map[string]bool)
	seenHeaders := make(map[string]bool)
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		tags, err := parseTags(structure.Tag(i))
		if err != nil {
			return nil, p.diagnostic(field.Pos(), err.Error())
		}
		for _, other := range forbidden {
			if _, exists := tags[other]; exists {
				return nil, p.diagnostic(field.Pos(), fmt.Sprintf("%s fields cannot declare %s tags", tag, other))
			}
		}
		fieldTag := tag
		if _, header := tags["header"]; tag == "query" && header {
			if _, query := tags["query"]; query {
				return nil, p.diagnostic(field.Pos(), "query fields cannot declare both query and header tags; use one source tag")
			}
			fieldTag = "header"
		}
		raw, explicit := tags[fieldTag]
		if raw == "-" {
			continue
		}
		if field.Embedded() || !field.Exported() {
			return nil, p.diagnostic(field.Pos(), fmt.Sprintf("%s fields must be exported and non-embedded; use %s:\"-\" to skip", fieldTag, fieldTag))
		}
		pieces := strings.Split(raw, ",")
		options := make(map[string]bool)
		for _, option := range pieces[1:] {
			if !allowed[option] || options[option] {
				return nil, p.diagnostic(field.Pos(), fmt.Sprintf("unknown or repeated %s tag option", fieldTag))
			}
			options[option] = true
		}
		name := pieces[0]
		if !explicit || name == "" && len(options) > 0 {
			name = snake(field.Name())
		}
		if fieldTag == "header" {
			if err := httpheader.ValidateRequestName(name); err != nil {
				return nil, p.diagnostic(field.Pos(), "header tag: "+err.Error())
			}
			name, err = httpheader.CanonicalName(name)
			if err != nil {
				return nil, p.diagnostic(field.Pos(), "header tag: "+err.Error())
			}
			if seenHeaders[name] {
				return nil, p.diagnostic(field.Pos(), "multiple fields bind the same header parameter (names are case-insensitive)")
			}
			seenHeaders[name] = true
		} else {
			if !httpquery.ValidName(name) {
				return nil, p.diagnostic(field.Pos(), fmt.Sprintf("%s tag requires a valid parameter name", tag))
			}
			if seen[name] {
				return nil, p.diagnostic(field.Pos(), fmt.Sprintf("multiple fields bind the same %s parameter", tag))
			}
			seen[name] = true
		}
		presentation, err := parsePresentation(tags["client"])
		if err != nil {
			return nil, p.diagnostic(field.Pos(), err.Error())
		}
		fields = append(fields, transportField{name: field.Name(), parameter: name, source: fieldTag, typ: field.Type(), position: field.Pos(), index: i, options: options, presentation: presentation})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].parameter == fields[j].parameter {
			return fields[i].source < fields[j].source
		}
		return fields[i].parameter < fields[j].parameter
	})
	return fields, nil
}
