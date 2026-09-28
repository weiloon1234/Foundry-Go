package contract

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonpointer"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// shapeLimits is separate from the parser's accounting: aliases consume work
// without consuming wire nodes. The adapter supplies both bounds explicitly.
type shapeLimits struct{ steps, issues int }

type shapeCheck struct {
	ctx    context.Context
	schema *compiledSchema
	limits shapeLimits
	steps  int
	issues []Issue
	err    error
}

// check inspects a tree already accepted by jsonwire. Syntax and wire resource
// validation must precede it. Each graph visit consumes work, including aliases
// and absent declared fields; no recursive alias expansion uses the Go stack.
func (s *compiledSchema) check(ctx context.Context, node any, limits shapeLimits) ([]Issue, error) {
	if ctx == nil || limits.steps <= 0 || limits.issues <= 0 {
		return nil, fault.New(fault.Invalid, "invalid JSON contract validation bounds")
	}
	check := shapeCheck{ctx: ctx, schema: s, limits: limits}
	check.visit(s.description.Root, node, "", 0)
	if check.err != nil {
		return check.issues, check.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return check.issues, nil
}

func (c *shapeCheck) take() bool {
	if c.err != nil || len(c.issues) >= c.limits.issues {
		return false
	}
	if err := c.ctx.Err(); err != nil {
		c.err = err
		return false
	}
	if c.steps >= c.limits.steps {
		c.err = fault.New(fault.Invalid, "JSON contract validation work bound exceeded")
		return false
	}
	c.steps++
	return true
}

func (c *shapeCheck) issue(path string, code IssueCode) {
	if len(c.issues) < c.limits.issues {
		c.issues = append(c.issues, Issue{Path: path, Code: code})
	}
}

func childPath(parent, key string) string {
	return jsonpointer.Append(parent, key)
}

func (c *shapeCheck) visit(id TypeID, node any, path string, depth int) {
	if depth > jsonwire.MaxDepth {
		c.err = fault.New(fault.Invalid, "JSON contract validation depth bound exceeded")
		return
	}
	var typ Type
	for {
		if !c.take() {
			return
		}
		typ = c.schema.types[id]
		if node == nil {
			if !typ.Nullable {
				c.issue(path, NullIssue)
			}
			return
		}
		if typ.Kind != AliasKind {
			break
		}
		id = typ.Element
	}
	switch typ.Kind {
	case DynamicKind:
		// The wire parser already bounded and validated this explicit dynamic tree.
	case ObjectKind:
		object, ok := node.(map[string]any)
		if !ok {
			c.issue(path, TypeIssue)
			return
		}
		c.object(typ, object, path, depth, "")
	case UnionKind:
		object, ok := node.(map[string]any)
		if !ok {
			c.issue(path, TypeIssue)
			return
		}
		tagPath := childPath(path, typ.Discriminator)
		raw, exists := object[typ.Discriminator]
		if !exists {
			c.issue(tagPath, RequiredIssue)
			return
		}
		tag, ok := raw.(string)
		if !ok {
			c.issue(tagPath, TypeIssue)
			return
		}
		variant := c.schema.variants[id][tag]
		if variant == "" {
			c.issue(tagPath, ValueIssue)
			return
		}
		if !c.take() {
			return
		}
		c.object(c.schema.types[variant], object, path, depth, typ.Discriminator)
	case ArrayKind:
		array, ok := node.([]any)
		if !ok {
			c.issue(path, TypeIssue)
			return
		}
		if size, fixed := typ.Length.Get(); fixed && len(array) != size {
			c.issue(path, LengthIssue)
			return
		}
		for i, child := range array {
			if c.err != nil || len(c.issues) >= c.limits.issues {
				return
			}
			c.visit(typ.Element, child, childPath(path, strconv.Itoa(i)), depth+1)
		}
	case MapKind:
		object, ok := node.(map[string]any)
		if !ok {
			c.issue(path, TypeIssue)
			return
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			if !c.take() {
				return
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !c.checkMapKeys(typ, keys, path) {
			return
		}
		for _, key := range keys {
			if c.err != nil || len(c.issues) >= c.limits.issues {
				return
			}
			c.visit(typ.Element, object[key], childPath(path, key), depth+1)
		}
	case QuotedKind:
		text, ok := node.(string)
		if !ok {
			c.issue(path, TypeIssue)
			return
		}
		inner, err := jsonwire.Decode([]byte(text), jsonwire.Limits{Bytes: len(text), Depth: 0, Nodes: 1})
		if err != nil || inner == nil {
			c.issue(path, ValueIssue)
			return
		}
		c.visit(typ.Element, inner, path, depth)
	default:
		if !scalarType(typ.Kind, node) {
			c.issue(path, TypeIssue)
			return
		}
		if !scalarValid(typ, node) {
			c.issue(path, ValueIssue)
			return
		}
		if len(typ.Cases) != 0 {
			key, err := scalarCase(typ, node)
			if err != nil || !c.schema.cases[id][string(key)] {
				c.issue(path, ValueIssue)
			}
		}
	}
}

func scalarType(kind Kind, node any) bool {
	switch kind {
	case BooleanKind:
		_, ok := node.(bool)
		return ok
	case StringKind:
		_, ok := node.(string)
		return ok
	case IntegerKind, NumberKind:
		_, ok := node.(json.Number)
		return ok
	default:
		return false
	}
}

func (c *shapeCheck) object(typ Type, object map[string]any, path string, depth int, discriminator string) {
	unknown := false
	for key := range object {
		if !c.take() {
			return
		}
		if _, known := c.schema.properties[typ.ID][key]; !known && (discriminator == "" || key != discriminator) {
			unknown = true
		}
	}
	if unknown {
		c.issue(path, UnknownIssue)
	}
	for _, property := range typ.Properties {
		if !c.take() {
			return
		}
		child, exists := object[property.Name]
		if !exists {
			if property.Required {
				c.issue(childPath(path, property.Name), RequiredIssue)
			}
			continue
		}
		c.visit(property.Type, child, childPath(path, property.Name), depth+1)
	}
}
