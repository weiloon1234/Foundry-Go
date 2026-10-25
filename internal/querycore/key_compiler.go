package querycore

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Keys are local to one SELECT. Reusing their emitted SQL also reuses parameter
// identities, as required for PostgreSQL grouping and DISTINCT ON matching.
type CompiledKey struct {
	Node        ValueExpression
	Grouped     bool
	Sql         string
	ArgumentEnd int
	Rendering   bool
}

type SelectKeys struct {
	Index      ValueIndex
	Entries    []*CompiledKey
	Fields     map[FieldRef]*CompiledKey
	Kinds      map[reflect.Type]bool
	GroupCount int
}

func (c *Compiler) RegisterKey(v ValueExpression, grouped bool) (*CompiledKey, error) {
	if c.Keys == nil {
		c.Keys = &SelectKeys{Fields: make(map[FieldRef]*CompiledKey), Kinds: make(map[reflect.Type]bool)}
	}
	p, err := c.PlanValue(v)
	if err != nil {
		return nil, err
	}
	i, found, err := c.Keys.Index.Find(p)
	if err != nil {
		return nil, err
	}
	if !found {
		if len(c.Keys.Entries) >= MaxExpressionNodes {
			return nil, safeerror.NewFault(fault.Invalid, "select keys exceed their resource bound")
		}
		i = c.Keys.Index.Add(p)
		entry := &CompiledKey{Node: v}
		c.Keys.Entries = append(c.Keys.Entries, entry)
		if f, ok := v.(FieldRef); ok {
			c.Keys.Fields[f] = entry
		}
		c.Keys.Kinds[reflect.TypeOf(v)] = true
	}
	entry := c.Keys.Entries[i]
	if grouped && !entry.Grouped {
		entry.Grouped = true
		c.Keys.GroupCount++
	}
	return entry, nil
}

func (c *Compiler) MatchingKey(v ValueExpression, groupedOnly bool) (*CompiledKey, error) {
	if c.Keys == nil || (groupedOnly && c.Keys.GroupCount == 0) {
		return nil, nil
	}
	var entry *CompiledKey
	if f, ok := v.(FieldRef); ok {
		entry = c.Keys.Fields[f]
	} else {
		if !c.Keys.Kinds[reflect.TypeOf(v)] {
			return nil, nil
		}
		p, err := c.PlanValue(v)
		if err != nil {
			return nil, err
		}
		i, found, err := c.Keys.Index.Find(p)
		if err != nil || !found {
			return nil, err
		}
		entry = c.Keys.Entries[i]
	}
	if entry != nil && groupedOnly && !entry.Grouped {
		return nil, nil
	}
	return entry, nil
}

func (c *Compiler) KeySQL(key *CompiledKey, grouped map[FieldRef]bool, grouping bool) (string, error) {
	if key.Sql != "" {
		return key.Sql, nil
	}
	if key.Rendering {
		return "", safeerror.NewFault(fault.Invalid, "recursive expression key")
	}
	key.Rendering = true
	defer func() { key.Rendering = false }()
	// A matched GROUP BY expression is a unit. Its internal row columns need
	// not be independently grouped. Nested keys still share their parameters.
	if key.Grouped {
		grouping = false
	}
	text, err := c.RenderSelectedExpression(key.Node, grouped, grouping)
	if err != nil {
		return "", err
	}
	key.Sql, key.ArgumentEnd = text, len(c.Arguments)
	return text, nil
}

func (c *Compiler) DiscardArguments(start int) {
	c.Arguments = c.Arguments[:start]
	if c.Keys != nil {
		for _, key := range c.Keys.Entries {
			if key.ArgumentEnd > start {
				key.Sql = ""
			}
		}
	}
}

func (c *Compiler) GroupKeys(values []ValueExpression) (map[FieldRef]bool, error) {
	fields := make(map[FieldRef]bool, len(values))
	seen := make(map[*CompiledKey]bool, len(values))
	for _, v := range values {
		if err := ValidateRowValue(v, c.DeclaredField, 0, &c.ExpressionNodes); err != nil {
			return nil, err
		}
		key, err := c.RegisterKey(v, true)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, safeerror.NewFault(fault.Invalid, "repeated GROUP BY key")
		}
		seen[key] = true
		if f, ok := v.(FieldRef); ok {
			fields[f] = true
		}
	}
	return fields, nil
}
