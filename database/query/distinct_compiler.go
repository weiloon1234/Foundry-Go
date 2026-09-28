package query

import (
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (c *compiler) distinctSQL(s selectNode, grouped map[fieldRef]bool, grouping bool) (string, error) {
	d := s.distinct
	switch d.kind {
	case noDistinct, distinctRows:
		if len(d.keys) != 0 {
			return "", fault.New(fault.Invalid, "unexpected DISTINCT keys")
		}
		if d.kind == distinctRows {
			return "DISTINCT ", nil
		}
		return "", nil
	case distinctOn:
		if len(d.keys) == 0 || len(d.keys) > MaxExpressionNodes {
			return "", fault.New(fault.Invalid, "DISTINCT ON requires bounded, nonempty keys")
		}
	default:
		return "", fault.New(fault.Invalid, "invalid DISTINCT mode")
	}
	keys := make(map[*compiledKey]bool, len(d.keys))
	entries := make([]*compiledKey, len(d.keys))
	for i, node := range d.keys {
		if err := c.validateSelectedExpression(node, grouped, grouping); err != nil {
			return "", err
		}
		key, err := c.registerKey(node, false)
		if err != nil {
			return "", err
		}
		if keys[key] {
			return "", fault.New(fault.Invalid, "repeated DISTINCT ON key")
		}
		keys[key], entries[i] = true, key
	}
	names := make([]string, len(entries))
	for i, key := range entries {
		text, err := c.keySQL(key, grouped, grouping)
		if err != nil {
			return "", err
		}
		names[i] = text
	}
	// PostgreSQL accepts a permutation or a shortened prefix of the keys, but
	// another expression may only follow after every key has been ordered.
	remaining := len(keys)
	for _, order := range s.orders {
		key, err := c.matchingKey(order.expression, false)
		if err != nil {
			return "", err
		}
		if keys[key] {
			keys[key] = false
			remaining--
		} else if remaining != 0 {
			// A repeated key is still part of the prefix.
			if _, found := keys[key]; !found {
				return "", fault.New(fault.Invalid, "DISTINCT ON keys must precede other ORDER BY expressions")
			}
		}
	}
	return "DISTINCT ON (" + strings.Join(names, ", ") + ") ", nil
}

// Match SQL emitted by the single expression compiler, including encoded
// bindings. Sorting by the matched output ordinal reuses its parameters instead
// of emitting equivalent expressions with different PostgreSQL parameter IDs.
type distinctOrder struct {
	index    valueIndex
	ordinals []int
}

func (d *distinctOrder) add(plan plannedValue, ordinal int) {
	d.index.add(plan)
	d.ordinals = append(d.ordinals, ordinal)
}

func (d *distinctOrder) compile(c *compiler, order orderNode, grouped map[fieldRef]bool, grouping bool) (string, error) {
	if err := c.validateSelectedExpression(order.expression, grouped, grouping); err != nil {
		return "", err
	}
	plan, err := c.planValue(order.expression)
	if err != nil {
		return "", err
	}
	i, found, err := d.index.find(plan)
	if err != nil {
		return "", err
	}
	if found {
		return strconv.Itoa(d.ordinals[i]), nil
	}
	return "", fault.New(fault.Invalid, "DISTINCT ordering must match a selected expression and its bindings")
}
