package generate

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
)

// transportField owns a concrete struct field and its exact wire name. URL
// query and multipart discovery share tag parsing, visibility and collisions.
type transportField struct {
	name, parameter string
	typ             types.Type
	position        token.Pos
	index           int
	options         map[string]bool
	presentation    contract.Presentation
}

func discoverTransportFields(p *packageInput, structure *types.Struct, tag string, allowed map[string]bool, forbidden []string) ([]transportField, error) {
	var fields []transportField
	seen := make(map[string]bool)
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
		raw, explicit := tags[tag]
		if raw == "-" {
			continue
		}
		if field.Embedded() || !field.Exported() {
			return nil, p.diagnostic(field.Pos(), fmt.Sprintf("%s fields must be exported and non-embedded; use %s:\"-\" to skip", tag, tag))
		}
		pieces := strings.Split(raw, ",")
		options := make(map[string]bool)
		for _, option := range pieces[1:] {
			if !allowed[option] || options[option] {
				return nil, p.diagnostic(field.Pos(), fmt.Sprintf("unknown or repeated %s tag option", tag))
			}
			options[option] = true
		}
		name := pieces[0]
		if !explicit || name == "" && len(options) > 0 {
			name = snake(field.Name())
		}
		if !httpquery.ValidName(name) {
			return nil, p.diagnostic(field.Pos(), fmt.Sprintf("%s tag requires a valid parameter name", tag))
		}
		if seen[name] {
			return nil, p.diagnostic(field.Pos(), fmt.Sprintf("multiple fields bind the same %s parameter", tag))
		}
		seen[name] = true
		presentation, err := parsePresentation(tags["client"])
		if err != nil {
			return nil, p.diagnostic(field.Pos(), err.Error())
		}
		fields = append(fields, transportField{name: field.Name(), parameter: name, typ: field.Type(), position: field.Pos(), index: i, options: options, presentation: presentation})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].parameter < fields[j].parameter })
	return fields, nil
}
