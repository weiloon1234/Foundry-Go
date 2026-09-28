package query

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Keys are local to one SELECT. Reusing their emitted SQL also reuses parameter
// identities, as required for PostgreSQL grouping and DISTINCT ON matching.
type compiledKey struct {
	node        valueExpression
	grouped     bool
	sql         string
	argumentEnd int
	rendering   bool
}

type selectKeys struct {
	index      valueIndex
	entries    []*compiledKey
	fields     map[fieldRef]*compiledKey
	kinds      map[reflect.Type]bool
	groupCount int
}

func (c *compiler) registerKey(v valueExpression, grouped bool) (*compiledKey, error) {
	if c.keys == nil {
		c.keys = &selectKeys{fields: make(map[fieldRef]*compiledKey), kinds: make(map[reflect.Type]bool)}
	}
	p, err := c.planValue(v)
	if err != nil {
		return nil, err
	}
	i, found, err := c.keys.index.find(p)
	if err != nil {
		return nil, err
	}
	if !found {
		if len(c.keys.entries) >= MaxExpressionNodes {
			return nil, fault.New(fault.Invalid, "select keys exceed their resource bound")
		}
		i = c.keys.index.add(p)
		entry := &compiledKey{node: v}
		c.keys.entries = append(c.keys.entries, entry)
		if f, ok := v.(fieldRef); ok {
			c.keys.fields[f] = entry
		}
		c.keys.kinds[reflect.TypeOf(v)] = true
	}
	entry := c.keys.entries[i]
	if grouped && !entry.grouped {
		entry.grouped = true
		c.keys.groupCount++
	}
	return entry, nil
}

func (c *compiler) matchingKey(v valueExpression, groupedOnly bool) (*compiledKey, error) {
	if c.keys == nil || (groupedOnly && c.keys.groupCount == 0) {
		return nil, nil
	}
	var entry *compiledKey
	if f, ok := v.(fieldRef); ok {
		entry = c.keys.fields[f]
	} else {
		if !c.keys.kinds[reflect.TypeOf(v)] {
			return nil, nil
		}
		p, err := c.planValue(v)
		if err != nil {
			return nil, err
		}
		i, found, err := c.keys.index.find(p)
		if err != nil || !found {
			return nil, err
		}
		entry = c.keys.entries[i]
	}
	if entry != nil && groupedOnly && !entry.grouped {
		return nil, nil
	}
	return entry, nil
}

func (c *compiler) keySQL(key *compiledKey, grouped map[fieldRef]bool, grouping bool) (string, error) {
	if key.sql != "" {
		return key.sql, nil
	}
	if key.rendering {
		return "", fault.New(fault.Invalid, "recursive expression key")
	}
	key.rendering = true
	defer func() { key.rendering = false }()
	// A matched GROUP BY expression is a unit. Its internal row columns need
	// not be independently grouped. Nested keys still share their parameters.
	if key.grouped {
		grouping = false
	}
	text, err := c.renderSelectedExpression(key.node, grouped, grouping)
	if err != nil {
		return "", err
	}
	key.sql, key.argumentEnd = text, len(c.arguments)
	return text, nil
}

func (c *compiler) discardArguments(start int) {
	c.arguments = c.arguments[:start]
	if c.keys != nil {
		for _, key := range c.keys.entries {
			if key.argumentEnd > start {
				key.sql = ""
			}
		}
	}
}

func (c *compiler) groupKeys(values []valueExpression) (map[fieldRef]bool, error) {
	fields := make(map[fieldRef]bool, len(values))
	seen := make(map[*compiledKey]bool, len(values))
	for _, v := range values {
		if err := validateRowValue(v, c.declaredField, 0, &c.expressionNodes); err != nil {
			return nil, err
		}
		key, err := c.registerKey(v, true)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fault.New(fault.Invalid, "repeated GROUP BY key")
		}
		seen[key] = true
		if f, ok := v.(fieldRef); ok {
			fields[f] = true
		}
	}
	return fields, nil
}
